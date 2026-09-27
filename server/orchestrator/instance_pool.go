package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"speektome-server/config"
)

type WorkerState string

const (
	StateIdle         WorkerState = "IDLE"
	StateProvisioning WorkerState = "PROVISIONING"
	StateActive       WorkerState = "ACTIVE"
	StateRebalancing  WorkerState = "REBALANCING"
	StateDraining     WorkerState = "DRAINING"
	StateTerminated   WorkerState = "TERMINATED"
)

// DefaultRegionalAMIs holds the pre-baked Golden AMIs with Nemotron 3.5 INT8 runtime for each region.
var DefaultRegionalAMIs = map[string]string{
	"us-east-1": "ami-REDACTED", // SpeekToMe-Nemotron-INT8-UltraLean-v1 (20GB gp3 / 6.6GB data)
}

type SpotWorker struct {
	InstanceID       string      `json:"instance_id"`
	Region           string      `json:"region"`
	AvailabilityZone string      `json:"availability_zone"`
	InstanceType     string      `json:"instance_type"`
	IPAddress        string      `json:"ip_address"`
	EndpointURL      string      `json:"endpoint_url"`
	State            WorkerState `json:"state"`
	SpotPriceUSD     float64     `json:"spot_price_usd"`
	LatencyMs        float64     `json:"latency_ms"`
	LaunchedAt       time.Time   `json:"launched_at"`
	LastHeartbeat    time.Time   `json:"last_heartbeat"`
}

type Orchestrator struct {
	mu           sync.RWMutex
	spotManager  SpotManager
	activeWorker *SpotWorker
	drainingList []*SpotWorker
	maxLatency   time.Duration
	amiID        string
	keyName      string
	secGroup     string
	userData     string
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewOrchestrator(sm SpotManager, amiID, keyName, secGroup, userData string) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())
	if sm == nil {
		sm = NewAWSSpotManager()
	}
	return &Orchestrator{
		spotManager: sm,
		maxLatency:  80 * time.Millisecond,
		amiID:       amiID,
		keyName:     keyName,
		secGroup:    secGroup,
		userData:    userData,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetMaxLatency configures the maximum tolerable region latency.
func (o *Orchestrator) SetMaxLatency(d time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.maxLatency = d
}

// Start begins the background reconciliation loop.
func (o *Orchestrator) Start() {
	go o.reconciliationLoop()
}

// Stop terminates background jobs.
func (o *Orchestrator) Stop() {
	o.cancel()
}

func (o *Orchestrator) reconciliationLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			o.Reconcile(o.ctx)
		}
	}
}

// Reconcile evaluates desired state vs live spot instances.
func (o *Orchestrator) Reconcile(ctx context.Context) {
	cfg := config.ConfigManager.Get()
	enabled := cfg.InferenceInstanceEnabled

	o.mu.Lock()
	defer o.mu.Unlock()

	// 1. If disabled: scale down all instances
	if !enabled {
		if o.activeWorker != nil && o.activeWorker.State != StateTerminated {
			log.Printf("[ORCHESTRATOR] Inference instances disabled in config. Terminating active worker %s (%s)...",
				o.activeWorker.InstanceID, o.activeWorker.Region)
			_ = o.spotManager.TerminateWorker(ctx, o.activeWorker.Region, o.activeWorker.InstanceID)
			o.activeWorker.State = StateTerminated
			o.activeWorker = nil
		}
		for _, dw := range o.drainingList {
			if dw.State != StateTerminated {
				_ = o.spotManager.TerminateWorker(ctx, dw.Region, dw.InstanceID)
				dw.State = StateTerminated
			}
		}
		o.drainingList = nil
		return
	}

	// 2. If enabled: ensure we have an active healthy worker
	if o.activeWorker == nil || o.activeWorker.State == StateTerminated {
		log.Printf("[ORCHESTRATOR] Inference enabled but no active worker. Discovering cheapest low-latency Spot pool (<80ms)...")
		o.provisionNewWorkerLocked(ctx)
		return
	}

	// Check heartbeat staleness
	if o.activeWorker.State == StateActive && time.Since(o.activeWorker.LastHeartbeat) > 3*time.Minute {
		log.Printf("[ORCHESTRATOR WARNING] Worker %s heartbeat stale (>3m). Initiating replacement...", o.activeWorker.InstanceID)
		o.TriggerRebalanceLocked(ctx, "Heartbeat timeout")
	}
}

func (o *Orchestrator) provisionNewWorkerLocked(ctx context.Context) {
	validRegions := GetValidLowLatencyRegions(ctx, DefaultCandidateRegions, o.maxLatency)
	if len(validRegions) == 0 {
		log.Printf("[ORCHESTRATOR ERROR] No valid regions found within latency threshold (%v)", o.maxLatency)
		return
	}

	pool, err := o.spotManager.FindCheapestValidPool(ctx, validRegions, []string{"g6f.large"})
	if err != nil {
		log.Printf("[ORCHESTRATOR ERROR] Failed to find cheapest pool: %v", err)
		return
	}

	targetAMI := o.amiID
	if targetAMI == "" {
		targetAMI = DefaultRegionalAMIs[pool.Region]
	}

	instID, err := o.spotManager.LaunchSpotWorker(ctx, pool, targetAMI, o.keyName, o.secGroup, o.userData)
	if err != nil {
		log.Printf("[ORCHESTRATOR ERROR] Failed to launch spot worker: %v", err)
		return
	}

	o.activeWorker = &SpotWorker{
		InstanceID:       instID,
		Region:           pool.Region,
		AvailabilityZone: pool.AvailabilityZone,
		InstanceType:     pool.InstanceType,
		State:            StateProvisioning,
		SpotPriceUSD:     pool.SpotPriceUSD,
		LatencyMs:        pool.LatencyMs,
		LaunchedAt:       time.Now(),
		LastHeartbeat:    time.Now(),
	}
}

// RegisterHeartbeat records an active heartbeat from a booted worker.
func (o *Orchestrator) RegisterHeartbeat(instanceID, region, ip string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.activeWorker != nil && o.activeWorker.InstanceID == instanceID {
		o.activeWorker.IPAddress = ip
		o.activeWorker.EndpointURL = fmt.Sprintf("ws://%s:8080/v1/audio/stream", ip)
		o.activeWorker.State = StateActive
		o.activeWorker.LastHeartbeat = time.Now()

		// Dynamically update Go server in-memory config for streaming
		config.ConfigManager.UpdateSelfHostedStreamingURL(o.activeWorker.EndpointURL)
		log.Printf("[ORCHESTRATOR] Worker %s ACTIVE at %s (Endpoint: %s)", instanceID, ip, o.activeWorker.EndpointURL)
		return nil
	}

	return fmt.Errorf("instance %s is not the recognized active worker", instanceID)
}

// TriggerRebalance initiates failover when an AWS Rebalance/Interruption warning is received.
func (o *Orchestrator) TriggerRebalance(ctx context.Context, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.TriggerRebalanceLocked(ctx, reason)
}

func (o *Orchestrator) TriggerRebalanceLocked(ctx context.Context, reason string) {
	if o.activeWorker == nil || o.activeWorker.State == StateRebalancing || o.activeWorker.State == StateDraining {
		return
	}

	log.Printf("[ORCHESTRATOR FAILOVER] Triggering rebalance for %s (%s). Reason: %s",
		o.activeWorker.InstanceID, o.activeWorker.Region, reason)

	oldWorker := o.activeWorker
	oldWorker.State = StateDraining
	o.drainingList = append(o.drainingList, oldWorker)
	o.activeWorker = nil

	// Schedule termination of old instance after grace period (e.g. 2 minutes)
	go func(w *SpotWorker) {
		time.Sleep(2 * time.Minute)
		_ = o.spotManager.TerminateWorker(context.Background(), w.Region, w.InstanceID)
		w.State = StateTerminated
		log.Printf("[ORCHESTRATOR DRAIN] Drained and terminated old instance %s", w.InstanceID)
	}(oldWorker)

	// Immediately provision replacement
	o.provisionNewWorkerLocked(ctx)
}

// GetStatus returns the current live state of the orchestrator.
func (o *Orchestrator) GetStatus() map[string]interface{} {
	o.mu.RLock()
	defer o.mu.RUnlock()

	cfg := config.ConfigManager.Get()
	var active interface{}
	if o.activeWorker != nil {
		active = o.activeWorker
	}
	res := map[string]interface{}{
		"inference_instance_enabled": cfg.InferenceInstanceEnabled,
		"active_worker":              active,
		"draining_workers":           o.drainingList,
		"max_allowed_latency_ms":     float64(o.maxLatency.Microseconds()) / 1000.0,
	}
	return res
}
