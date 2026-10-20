package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	log "speektome-server/privacylog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type WorkerState string

const (
	StateProvisioning WorkerState = "PROVISIONING"
	StateActive       WorkerState = "ACTIVE"
	StateRebalancing  WorkerState = "REBALANCING"
	StateDraining     WorkerState = "DRAINING"
	StateUnhealthy    WorkerState = "UNHEALTHY"
	StateTerminated   WorkerState = "TERMINATED"
)

type RegionConfig struct {
	Region         string            `json:"region"`
	AMI            string            `json:"ami"`
	SecurityGroup  string            `json:"security_group"`
	Subnets        map[string]string `json:"subnets"` // AZ -> IPv6-enabled subnet
	ArtifactBucket string            `json:"artifact_bucket"`
	ArtifactRegion string            `json:"artifact_region"`
	EventQueueURL  string            `json:"event_queue_url"`
	UserData       string            `json:"user_data"` // encoded only by AWS adapter, not by callers
}
type FleetConfig struct {
	MeetingRequireIdle      bool           `json:"meeting_require_idle"`
	FleetID                 string         `json:"fleet_id"`
	AccountID               string         `json:"account_id"`
	Desired                 int            `json:"desired"`
	RootVolumeGiB           int64          `json:"root_volume_gib"`
	MaxInstances            int            `json:"max_instances"`
	MaxHourlyUSD            float64        `json:"max_hourly_usd"`
	MaxPoolHourlyUSD        float64        `json:"max_pool_hourly_usd"`
	MaxRTTMs                float64        `json:"max_rtt_ms"`
	InstanceType            string         `json:"instance_type"`
	InstanceProfile         string         `json:"instance_profile"`
	KeyName                 string         `json:"key_name"`
	TokenFile               string         `json:"token_file"`
	StateFile               string         `json:"state_file"`
	Listen                  string         `json:"listen"`
	ProvisionTimeoutSeconds int            `json:"provision_timeout_seconds"`
	HealthGraceSeconds      int            `json:"health_grace_seconds"`
	DrainTimeoutSeconds     int            `json:"drain_timeout_seconds"`
	Regions                 []RegionConfig `json:"regions"`
}

func (c *FleetConfig) defaults() {
	if c.RootVolumeGiB == 0 {
		c.RootVolumeGiB = 24
	}
	if c.Desired == 0 {
		c.Desired = 2
	}
	if c.MaxInstances == 0 {
		c.MaxInstances = c.Desired + 2
	}
	if c.InstanceType == "" {
		c.InstanceType = "g6f.large"
	}
	if c.MaxHourlyUSD == 0 {
		c.MaxHourlyUSD = .40
	}
	if c.MaxPoolHourlyUSD == 0 {
		c.MaxPoolHourlyUSD = .1
	}
	if c.ProvisionTimeoutSeconds == 0 {
		c.ProvisionTimeoutSeconds = 900
	}
	if c.HealthGraceSeconds == 0 {
		c.HealthGraceSeconds = 60
	}
	if c.DrainTimeoutSeconds == 0 {
		c.DrainTimeoutSeconds = 120
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8095"
	}
}
func (c FleetConfig) Validate() error {
	if c.FleetID == "" || c.AccountID == "" || c.StateFile == "" || c.TokenFile == "" {
		return errors.New("fleet_id, account_id, state_file and token_file required")
	}
	if c.Desired < 1 || c.MaxInstances < c.Desired+1 || c.MaxHourlyUSD <= 0 || c.MaxPoolHourlyUSD <= 0 {
		return errors.New("invalid capacity/budget; reserve at least one replacement slot")
	}
	if c.RootVolumeGiB < 15 || c.RootVolumeGiB > 128 {
		return errors.New("root_volume_gib must be between 15 and 128")
	}
	if c.InstanceType != "g6f.large" {
		return errors.New("only validated g6f.large profile enabled")
	}
	if len(c.Regions) == 0 || c.InstanceProfile == "" {
		return errors.New("regions and instance_profile required")
	}
	seen := map[string]bool{}
	for _, r := range c.Regions {
		if seen[r.Region] || r.Region == "" || r.AMI == "" || r.SecurityGroup == "" || len(r.Subnets) == 0 || r.UserData == "" || r.ArtifactBucket == "" || r.ArtifactRegion == "" {
			return fmt.Errorf("incomplete/duplicate region %s", r.Region)
		}
		seen[r.Region] = true
	}
	return nil
}
func LoadConfig(path string) (FleetConfig, error) {
	var c FleetConfig
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if e != nil {
		return c, e
	}
	c.defaults()
	return c, c.Validate()
}

type WorkerMetadata struct {
	Schema     int            `json:"schema"`
	InstanceID string         `json:"instance_id"`
	Region     string         `json:"region"`
	Ready      bool           `json:"ready"`
	Draining   bool           `json:"draining"`
	Active     int            `json:"active_streams"`
	Max        int            `json:"max_streams"`
	ASR        int            `json:"active_transcription_requests"`
	Diar       int            `json:"active_diarization_requests"`
	Batch      int            `json:"active_batch_requests"`
	Meetings   int            `json:"active_meeting_jobs"`
	Latency    map[string]any `json:"latency"`
	Queue      map[string]any `json:"queue"`
	SpotSignal *SpotSignal    `json:"spot_signal"`
}
type SpotSignal struct {
	Type string    `json:"type"`
	Time time.Time `json:"time"`
}
type SpotWorker struct {
	InstanceID       string         `json:"instance_id"`
	Region           string         `json:"region"`
	AvailabilityZone string         `json:"availability_zone"`
	InstanceType     string         `json:"instance_type"`
	IPAddress        string         `json:"ip_address"`
	EndpointURL      string         `json:"endpoint_url"`
	State            WorkerState    `json:"state"`
	SpotPriceUSD     float64        `json:"spot_price_usd"`
	LaunchedAt       time.Time      `json:"launched_at"`
	LastHealthy      time.Time      `json:"last_healthy"`
	UnhealthySince   time.Time      `json:"unhealthy_since,omitempty"`
	DrainSince       time.Time      `json:"drain_since,omitempty"`
	Rebalance        bool           `json:"rebalance"`
	Interruption     bool           `json:"interruption"`
	LatencyMs        float64        `json:"latency_ms"`
	Metadata         WorkerMetadata `json:"metadata"`
}
type SpotPoolCandidate struct {
	Region, AvailabilityZone, InstanceType string
	SpotPriceUSD, LatencyMs                float64
}
type CloudSnapshot struct {
	Workers  []SpotWorker
	Complete bool
}
type FleetCloud interface {
	Discover(context.Context, FleetConfig) (CloudSnapshot, error)
	Pools(context.Context, FleetConfig) ([]SpotPoolCandidate, error)
	Launch(context.Context, FleetConfig, SpotPoolCandidate, string) (SpotWorker, error)
	Terminate(context.Context, SpotWorker) error
}
type WorkerProbe interface {
	Metadata(context.Context, SpotWorker) (WorkerMetadata, float64, error)
	Drain(context.Context, SpotWorker) error
}
type pendingLaunch struct {
	Pool  SpotPoolCandidate `json:"pool"`
	Token string            `json:"token"`
	At    time.Time         `json:"at"`
}
type persistedState struct {
	Workers   map[string]SpotWorker `json:"workers"`
	Pending   *pendingLaunch        `json:"pending,omitempty"`
	Cooldowns map[string]time.Time  `json:"cooldowns"`
}
type Controller struct {
	mu            sync.RWMutex
	reconcile     sync.Mutex
	cfg           FleetConfig
	cloud         FleetCloud
	probe         WorkerProbe
	state         persistedState
	now           func() time.Time
	lastError     string
	reservations  map[string]int
	meetingLeases map[string]meetingLease
	ctx           context.Context
	cancel        context.CancelFunc
	wake          chan struct{}
}

func NewController(c FleetConfig, cloud FleetCloud, probe WorkerProbe) (*Controller, error) {
	c.defaults()
	if e := c.Validate(); e != nil {
		return nil, e
	}
	o := &Controller{cfg: c, cloud: cloud, probe: probe, now: time.Now, reservations: map[string]int{}, wake: make(chan struct{}, 1), state: persistedState{Workers: map[string]SpotWorker{}, Cooldowns: map[string]time.Time{}}}
	if b, e := os.ReadFile(c.StateFile); e == nil {
		if e = json.Unmarshal(b, &o.state); e != nil {
			return nil, fmt.Errorf("invalid persisted fleet state: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if o.state.Workers == nil {
		o.state.Workers = map[string]SpotWorker{}
	}
	if o.state.Cooldowns == nil {
		o.state.Cooldowns = map[string]time.Time{}
	}
	o.ctx, o.cancel = context.WithCancel(context.Background())
	return o, nil
}
func (o *Controller) saveLocked() error {
	b, e := json.MarshalIndent(o.state, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(o.cfg.StateFile), 0700); e != nil {
		return e
	}
	temp := o.cfg.StateFile + ".tmp"
	f, e := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(temp, o.cfg.StateFile); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(o.cfg.StateFile))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func poolKey(p SpotPoolCandidate) string { return p.Region + "/" + p.AvailabilityZone }
func newToken() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (o *Controller) Reconcile(ctx context.Context) error {
	o.reconcile.Lock()
	defer o.reconcile.Unlock()
	// Do not infer disappearance or create capacity after partial AWS discovery.
	snapshot, discoveryErr := o.cloud.Discover(ctx, o.cfg)
	if discoveryErr == nil && !snapshot.Complete {
		discoveryErr = errors.New("incomplete regional discovery")
	}
	var e error
	now := o.now()
	o.mu.Lock()
	if discoveryErr == nil {
		live := map[string]bool{}
		for _, w := range snapshot.Workers {
			live[w.InstanceID] = true
			if old, ok := o.state.Workers[w.InstanceID]; ok {
				w.State = old.State
				w.LastHealthy = old.LastHealthy
				w.UnhealthySince = old.UnhealthySince
				w.DrainSince = old.DrainSince
				w.Rebalance = old.Rebalance
				w.Interruption = w.Interruption || old.Interruption
				w.Metadata = old.Metadata
				w.LatencyMs = old.LatencyMs
				w.SpotPriceUSD = old.SpotPriceUSD
			} else if !w.Interruption {
				w.State = StateProvisioning
			}
			o.state.Workers[w.InstanceID] = w
		}
		for id := range o.state.Workers {
			if !live[id] {
				if w := o.state.Workers[id]; w.State == StateProvisioning && now.Sub(w.LaunchedAt) < 90*time.Second {
					continue
				}
				delete(o.state.Workers, id)
				delete(o.reservations, id)
			}
		}
	}
	workers := make([]SpotWorker, 0, len(o.state.Workers))
	for _, w := range o.state.Workers {
		workers = append(workers, w)
	}
	o.mu.Unlock()
	// Poll even draining workers: their active session count controls safe retirement.
	for _, w := range workers {
		if w.EndpointURL == "" {
			continue
		}
		m, rtt, err := o.probe.Metadata(ctx, w)
		o.mu.Lock()
		current, ok := o.state.Workers[w.InstanceID]
		if !ok {
			o.mu.Unlock()
			continue
		}
		if err == nil && m.InstanceID == w.InstanceID && m.Region == w.Region && m.Schema >= 2 {
			current.Metadata = m
			current.LatencyMs = rtt
			if m.SpotSignal != nil {
				o.signalLocked(&current, m.SpotSignal.Type, now)
			}
			if m.Ready && !m.Draining && !current.Interruption && !current.Rebalance {
				current.State = StateActive
				current.LastHealthy = now
				current.UnhealthySince = time.Time{}
				current.DrainSince = time.Time{}
			} else if current.Interruption || m.Draining {
				current.State = StateDraining
				if current.DrainSince.IsZero() {
					current.DrainSince = now
				}
			} else if !m.Ready && current.State != StateProvisioning && current.State != StateRebalancing {
				if current.UnhealthySince.IsZero() {
					current.UnhealthySince = now
				}
				current.State = StateUnhealthy
			}
		} else if current.State == StateActive || current.State == StateUnhealthy {
			if current.UnhealthySince.IsZero() {
				current.UnhealthySince = now
			}
			current.State = StateUnhealthy
		}
		o.state.Workers[w.InstanceID] = current
		o.mu.Unlock()
	}
	if discoveryErr != nil {
		// Keep the data plane healthy using known, certificate-pinned workers even
		// when an AWS regional control-plane API is unavailable. Mutations still stop.
		o.mu.Lock()
		saveErr := o.saveLocked()
		o.mu.Unlock()
		if saveErr != nil {
			return o.failure(saveErr)
		}
		return o.failure(discoveryErr)
	}
	// Failed cold boots have never served sessions; retire them even if the
	// fleet is undersized, otherwise repeated failures can exhaust the hard cap.
	o.mu.RLock()
	failed := []SpotWorker{}
	for _, w := range o.state.Workers {
		if w.State == StateProvisioning && now.Sub(w.LaunchedAt) >= time.Duration(o.cfg.ProvisionTimeoutSeconds)*time.Second {
			failed = append(failed, w)
		}
	}
	o.mu.RUnlock()
	for _, w := range failed {
		if err := o.cloud.Terminate(ctx, w); err != nil {
			return o.failure(err)
		}
		o.mu.Lock()
		delete(o.state.Workers, w.InstanceID)
		o.state.Cooldowns[w.Region+"/"+w.AvailabilityZone] = now.Add(5 * time.Minute)
		err := o.saveLocked()
		o.mu.Unlock()
		if err != nil {
			return err
		}
	}
	o.mu.Lock()
	counted := 0
	ready := 0
	totalCost := 0.0
	regions := map[string]int{}
	occupiedPools := map[string]bool{}
	for _, w := range o.state.Workers {
		totalCost += o.cfg.MaxPoolHourlyUSD
		occupiedPools[w.Region+"/"+w.AvailabilityZone] = true
		if w.State == StateActive {
			ready++
			counted++
			regions[w.Region]++
		}
		if w.State == StateProvisioning && now.Sub(w.LaunchedAt) < time.Duration(o.cfg.ProvisionTimeoutSeconds)*time.Second {
			counted++
			regions[w.Region]++
		}
		if w.State == StateUnhealthy && now.Sub(w.UnhealthySince) < time.Duration(o.cfg.HealthGraceSeconds)*time.Second {
			counted++
			regions[w.Region]++
		}
	}
	// Resume an ambiguous RunInstances using the same client token; never launch
	// a different pool until this request is resolved.
	pending := o.state.Pending
	need := counted < o.cfg.Desired && len(o.state.Workers) < o.cfg.MaxInstances
	o.mu.Unlock()
	if pending != nil {
		return o.launch(ctx, *pending)
	}
	if need {
		pools, err := o.cloud.Pools(ctx, o.cfg)
		if err != nil {
			return o.failure(err)
		}
		sort.SliceStable(pools, func(i, j int) bool {
			a, b := pools[i], pools[j]
			if (regions[a.Region] == 0) != (regions[b.Region] == 0) {
				return regions[a.Region] == 0
			}
			if occupiedPools[poolKey(a)] != occupiedPools[poolKey(b)] {
				return !occupiedPools[poolKey(a)]
			}
			return a.SpotPriceUSD < b.SpotPriceUSD
		})
		for _, p := range pools {
			o.mu.RLock()
			until := o.state.Cooldowns[poolKey(p)]
			o.mu.RUnlock()
			if now.Before(until) || p.SpotPriceUSD <= 0 || p.SpotPriceUSD > o.cfg.MaxPoolHourlyUSD || totalCost+o.cfg.MaxPoolHourlyUSD > o.cfg.MaxHourlyUSD || (o.cfg.MaxRTTMs > 0 && p.LatencyMs > o.cfg.MaxRTTMs) {
				continue
			}
			req := pendingLaunch{p, newToken(), now}
			o.mu.Lock()
			o.state.Pending = &req
			err = o.saveLocked()
			o.mu.Unlock()
			if err != nil {
				return o.failure(err)
			}
			err = o.launch(ctx, req)
			if err == nil {
				return nil
			} // one launch per pass; immediate wake fills remaining slots
			log.Printf("fleet pool %s rejected: %v", poolKey(p), err)
			var definite *DefiniteLaunchError
			if !errors.As(err, &definite) {
				return err
			}
		}
		return o.failure(errors.New("no eligible capacity within configured budget, latency and cooldown limits"))
	}
	// Only retire a risk/unhealthy worker after enough replacements are ready.
	// A final AWS interruption is not a reason to force our own premature kill.
	if ready >= o.cfg.Desired {
		for _, w := range workers {
			o.mu.RLock()
			w = o.state.Workers[w.InstanceID]
			o.mu.RUnlock()
			eligible := w.Rebalance || w.Interruption || w.State == StateUnhealthy || (w.State == StateDraining && !w.UnhealthySince.IsZero()) || (w.State == StateProvisioning && now.Sub(w.LaunchedAt) >= time.Duration(o.cfg.ProvisionTimeoutSeconds)*time.Second)
			if !eligible {
				continue
			}
			if w.DrainSince.IsZero() {
				if err := o.probe.Drain(ctx, w); err != nil && w.State != StateUnhealthy {
					continue
				}
				o.mu.Lock()
				w.State = StateDraining
				w.DrainSince = now
				o.state.Workers[w.InstanceID] = w
				o.mu.Unlock()
				continue
			}
			o.mu.RLock()
			active := max(w.Metadata.Active, o.reservations[w.InstanceID]) + w.Metadata.Batch
			o.mu.RUnlock()
			// A recommendation alone does not justify killing a healthy long session.
			forced := (w.Interruption || !w.UnhealthySince.IsZero()) && now.Sub(w.DrainSince) >= time.Duration(o.cfg.DrainTimeoutSeconds)*time.Second
			if active == 0 || forced {
				if err := o.cloud.Terminate(ctx, w); err != nil {
					return o.failure(err)
				}
				o.mu.Lock()
				delete(o.state.Workers, w.InstanceID)
				o.mu.Unlock()
			}
		}
	}
	o.mu.Lock()
	e = o.saveLocked()
	o.lastError = ""
	o.mu.Unlock()
	return e
}

type DefiniteLaunchError struct{ Err error }

func (e *DefiniteLaunchError) Error() string { return e.Err.Error() }
func (e *DefiniteLaunchError) Unwrap() error { return e.Err }
func (o *Controller) launch(ctx context.Context, p pendingLaunch) error {
	w, e := o.cloud.Launch(ctx, o.cfg, p.Pool, p.Token)
	o.mu.Lock()
	defer o.mu.Unlock()
	if e != nil {
		var definite *DefiniteLaunchError
		if errors.As(e, &definite) {
			o.state.Pending = nil
			o.state.Cooldowns[poolKey(p.Pool)] = o.now().Add(2 * time.Minute)
		}
		o.lastError = e.Error()
		if err := o.saveLocked(); err != nil {
			return err
		}
		return e
	}
	w.State = StateProvisioning
	w.SpotPriceUSD = p.Pool.SpotPriceUSD
	o.state.Workers[w.InstanceID] = w
	o.state.Pending = nil
	if e = o.saveLocked(); e != nil {
		return e
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
	return nil
}
func (o *Controller) failure(e error) error {
	o.mu.Lock()
	o.lastError = e.Error()
	o.mu.Unlock()
	return e
}
func (o *Controller) signalLocked(w *SpotWorker, kind string, now time.Time) {
	switch kind {
	case "EC2 Instance Rebalance Recommendation":
		w.Rebalance = true
		if !w.Interruption {
			w.State = StateRebalancing
		}
	case "EC2 Spot Instance Interruption Warning":
		w.Interruption = true
		w.State = StateDraining
		if w.DrainSince.IsZero() {
			w.DrainSince = now
		}
	}
}
func (o *Controller) Signal(id, region, kind string) error {
	if kind != "EC2 Instance Rebalance Recommendation" && kind != "EC2 Spot Instance Interruption Warning" {
		return errors.New("unsupported signal")
	}
	o.mu.Lock()
	w, ok := o.state.Workers[id]
	if !ok || w.Region != region {
		o.mu.Unlock()
		return errors.New("event is not for a managed worker")
	}
	o.signalLocked(&w, kind, o.now())
	o.state.Workers[id] = w
	e := o.saveLocked()
	o.mu.Unlock()
	if e != nil {
		return e
	}
	if kind == "EC2 Spot Instance Interruption Warning" {
		go func() {
			ctx, cancel := context.WithTimeout(o.ctx, 6*time.Second)
			defer cancel()
			if err := o.probe.Drain(ctx, w); err != nil {
				log.Printf("warning drain: %v", err)
			}
		}()
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
	return nil
}
func (o *Controller) Start() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			if e := o.Reconcile(o.ctx); e != nil {
				log.Printf("fleet reconcile: %v", e)
			}
			select {
			case <-o.ctx.Done():
				return
			case <-ticker.C:
			case <-o.wake:
			}
		}
	}()
}
func (o *Controller) Stop() { o.cancel() }
func (o *Controller) GetStatus() map[string]any {
	o.mu.RLock()
	defer o.mu.RUnlock()
	list := []SpotWorker{}
	for _, w := range o.state.Workers {
		list = append(list, w)
	}
	return map[string]any{"fleet_id": o.cfg.FleetID, "desired": o.cfg.Desired, "workers": list, "pending_launch": o.state.Pending, "last_error": o.lastError, "automatic_demand_scaling": false}
}

// Candidates is advisory. Worker-side admission remains authoritative.
func (o *Controller) Candidates() []SpotWorker {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := []SpotWorker{}
	for _, w := range o.state.Workers {
		if !o.meetingReservedLocked(w.InstanceID) && w.State == StateActive && w.Metadata.Ready && !w.Metadata.Draining && o.now().Sub(w.LastHealthy) < 30*time.Second && max(w.Metadata.Active, o.reservations[w.InstanceID]) < w.Metadata.Max {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		am, bm := o.workerHasMeetingLocked(a.InstanceID), o.workerHasMeetingLocked(b.InstanceID)
		if am != bm {
			return !am
		} // Prefer meeting-free workers; never move established streams.
		la := float64(max(a.Metadata.Active, o.reservations[a.InstanceID])) / float64(a.Metadata.Max)
		lb := float64(max(b.Metadata.Active, o.reservations[b.InstanceID])) / float64(b.Metadata.Max)
		if la == lb {
			return a.LatencyMs < b.LatencyMs
		}
		return la < lb
	})
	return out
}
func (o *Controller) reserve(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	w, ok := o.state.Workers[id]
	if !ok || o.meetingReservedLocked(id) || w.State != StateActive || max(w.Metadata.Active, o.reservations[id]) >= w.Metadata.Max {
		return false
	}
	o.reservations[id]++
	return true
}
func (o *Controller) release(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.reservations[id] > 0 {
		o.reservations[id]--
	}
}

// Retain main.go's construction/route API without mutating global model URLs or
// inference handlers. Fleet operation requires an explicit config file.
type Orchestrator struct {
	*Controller
	initError error
}

func NewOrchestrator(_ any, _, _, _, _ string) *Orchestrator {
	path := os.Getenv("SPEEKTOME_FLEET_CONFIG")
	if path == "" {
		return &Orchestrator{initError: errors.New("SPEEKTOME_FLEET_CONFIG is required; legacy auto-provisioning removed")}
	}
	c, e := LoadConfig(path)
	if e != nil {
		return &Orchestrator{initError: e}
	}
	cloud := NewAWSCloud()
	probe, e := NewHTTPProbe(c, cloud)
	if e != nil {
		return &Orchestrator{initError: e}
	}
	ctrl, e := NewController(c, cloud, probe)
	return &Orchestrator{Controller: ctrl, initError: e}
}
func (o *Orchestrator) Start() {
	if o.initError != nil {
		log.Printf("fleet disabled: %v", o.initError)
		return
	}
	o.Controller.Start()
}
func (o *Orchestrator) Stop() {
	if o.Controller != nil {
		o.Controller.Stop()
	}
}
func (o *Orchestrator) GetStatus() map[string]any {
	if o.Controller == nil {
		return map[string]any{"error": fmt.Sprint(o.initError)}
	}
	return o.Controller.GetStatus()
}

// Caller holds o.mu. Idle-only meeting leases reserve routing capacity before
// the uploaded audio reaches the worker, closing the admission race.
func (o *Controller) meetingReservedLocked(id string) bool {
	if !o.cfg.MeetingRequireIdle {
		return false
	}
	for _, lease := range o.meetingLeases {
		if lease.Worker == id && lease.Expires.After(o.now()) {
			return true
		}
	}
	return false
}

func (o *Controller) workerHasMeetingLocked(id string) bool {
	if o.state.Workers[id].Metadata.Meetings > 0 {
		return true
	}
	for _, lease := range o.meetingLeases {
		if lease.Worker == id && lease.Expires.After(o.now()) {
			return true
		}
	}
	return false
}
