package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeCloud struct {
	workers       []SpotWorker
	calls         []SpotPoolCandidate
	tokens        []string
	launchError   error
	discoverError error
	terminated    []string
	hideNew       bool
}

func (f *fakeCloud) Discover(context.Context, FleetConfig) (CloudSnapshot, error) {
	return CloudSnapshot{append([]SpotWorker(nil), f.workers...), f.discoverError == nil}, f.discoverError
}
func (f *fakeCloud) Pools(context.Context, FleetConfig) ([]SpotPoolCandidate, error) {
	return []SpotPoolCandidate{{Region: "us-west-2", AvailabilityZone: "us-west-2d", SpotPriceUSD: .04}, {Region: "us-east-1", AvailabilityZone: "us-east-1d", SpotPriceUSD: .06}}, nil
}
func (f *fakeCloud) Launch(_ context.Context, _ FleetConfig, p SpotPoolCandidate, token string) (SpotWorker, error) {
	f.calls = append(f.calls, p)
	f.tokens = append(f.tokens, token)
	if f.launchError != nil {
		return SpotWorker{}, f.launchError
	}
	w := SpotWorker{InstanceID: fmt.Sprintf("i-%d", len(f.calls)), Region: p.Region, AvailabilityZone: p.AvailabilityZone, LaunchedAt: time.Now(), EndpointURL: "https://[::1]:80"}
	if !f.hideNew {
		f.workers = append(f.workers, w)
	}
	return w, nil
}
func (f *fakeCloud) Terminate(_ context.Context, w SpotWorker) error {
	f.terminated = append(f.terminated, w.InstanceID)
	return nil
}

type fakeProbe struct {
	ready  bool
	active int
	err    error
	keys   map[string]string // instance ID -> advertised transport key
}

func (p *fakeProbe) Metadata(_ context.Context, w SpotWorker) (WorkerMetadata, float64, error) {
	return WorkerMetadata{Schema: 2, InstanceID: w.InstanceID, Region: w.Region, Ready: p.ready, Active: p.active, Max: 8, TransportKey: p.keys[w.InstanceID]}, 1, p.err
}
func (p *fakeProbe) Drain(context.Context, SpotWorker) error { return nil }
func testConfig(t *testing.T) FleetConfig {
	t.Helper()
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	os.WriteFile(token, []byte("012345678901234567890123456789012345"), 0600)
	c := FleetConfig{FleetID: "test", AccountID: "123", StateFile: filepath.Join(dir, "state.json"), TokenFile: token, InstanceProfile: "profile", Regions: []RegionConfig{{Region: "us-west-2", AMI: "ami-test", SecurityGroup: "sg-test", Subnets: map[string]string{"us-west-2d": "subnet-1"}, ArtifactBucket: "bucket", ArtifactRegion: "us-east-1", UserData: "boot"}, {Region: "us-east-1", AMI: "ami-test", SecurityGroup: "sg-test", Subnets: map[string]string{"us-east-1d": "subnet-2"}, ArtifactBucket: "bucket", ArtifactRegion: "us-east-1", UserData: "boot"}}}
	c.defaults()
	// Fleet and routing tests use keyless fake workers; transport_test.go covers "required".
	c.TranscriptionEncryption = "preferred"
	return c
}
func TestTwoWorkersPreferDifferentRegionsAndNoDemandScaling(t *testing.T) {
	cloud := &fakeCloud{}
	probe := &fakeProbe{ready: true}
	o, e := NewController(testConfig(t), cloud, probe)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Stop()
	for range 4 {
		if e = o.Reconcile(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	if len(cloud.calls) != 2 || cloud.calls[0].Region == cloud.calls[1].Region {
		t.Fatalf("launches %+v", cloud.calls)
	}
	probe.active = 8
	for range 3 {
		if e = o.Reconcile(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	if len(cloud.calls) != 2 || len(o.Candidates()) != 0 {
		t.Fatal("full workers caused demand scaling or were selected")
	}
}
func TestWarningTargetsOneWorkerAndDuplicateDoesNotLaunchTwice(t *testing.T) {
	cloud := &fakeCloud{}
	o, e := NewController(testConfig(t), cloud, &fakeProbe{ready: true, active: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer o.Stop()
	for range 3 {
		o.Reconcile(context.Background())
	}
	if e = o.Signal("i-1", "us-west-2", "EC2 Instance Rebalance Recommendation"); e != nil {
		t.Fatal(e)
	}
	o.Reconcile(context.Background())
	o.Signal("i-1", "us-west-2", "EC2 Instance Rebalance Recommendation")
	o.Reconcile(context.Background())
	if len(cloud.calls) != 3 {
		t.Fatalf("duplicate warning launched %d", len(cloud.calls))
	}
	if len(cloud.terminated) != 0 {
		t.Fatal("active risk worker terminated before draining")
	}
}
func TestPartialDiscoveryNeverCreatesOrDeletes(t *testing.T) {
	cloud := &fakeCloud{discoverError: errors.New("network")}
	o, _ := NewController(testConfig(t), cloud, &fakeProbe{})
	defer o.Stop()
	if o.Reconcile(context.Background()) == nil || len(cloud.calls) != 0 {
		t.Fatal("partial discovery mutated cloud")
	}
}
func TestAmbiguousLaunchTokenSurvivesControllerRestart(t *testing.T) {
	c := testConfig(t)
	cloud := &fakeCloud{launchError: errors.New("timeout")}
	o, _ := NewController(c, cloud, &fakeProbe{})
	if o.Reconcile(context.Background()) == nil {
		t.Fatal("missing timeout")
	}
	o.Stop()
	next, e := NewController(c, cloud, &fakeProbe{})
	if e != nil {
		t.Fatal(e)
	}
	defer next.Stop()
	next.Reconcile(context.Background())
	if len(cloud.tokens) != 2 || cloud.tokens[0] != cloud.tokens[1] {
		t.Fatal("ambiguous launch retried with a different token")
	}
}
func TestEventualConsistencyDoesNotDuplicateColdBoot(t *testing.T) {
	cloud := &fakeCloud{hideNew: true}
	o, _ := NewController(testConfig(t), cloud, &fakeProbe{})
	defer o.Stop()
	for range 4 {
		o.Reconcile(context.Background())
	}
	if len(cloud.calls) != 2 {
		t.Fatalf("eventual consistency launched %d", len(cloud.calls))
	}
}
func TestCapacityFailureFallsBackAndHardBudget(t *testing.T) {
	cloud := &fakeCloud{launchError: &DefiniteLaunchError{errors.New("capacity")}}
	o, _ := NewController(testConfig(t), cloud, &fakeProbe{})
	defer o.Stop()
	o.Reconcile(context.Background())
	if len(cloud.calls) != 2 {
		t.Fatal("did not try alternate region")
	}
	o.Reconcile(context.Background())
	if len(cloud.calls) != 2 {
		t.Fatal("failed pools not cooled down")
	}
	c := testConfig(t)
	c.MaxHourlyUSD = .09
	other := &fakeCloud{}
	q, _ := NewController(c, other, &fakeProbe{})
	defer q.Stop()
	q.Reconcile(context.Background())
	if len(other.calls) != 0 {
		t.Fatal("conservative Spot price ceiling exceeded budget")
	}
}
func TestUnhealthyReadinessTriggersReplacement(t *testing.T) {
	cloud := &fakeCloud{}
	p := &fakeProbe{ready: true}
	o, _ := NewController(testConfig(t), cloud, p)
	defer o.Stop()
	for range 3 {
		o.Reconcile(context.Background())
	}
	p.ready = false
	o.Reconcile(context.Background())
	o.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	o.Reconcile(context.Background())
	if len(cloud.calls) != 3 {
		t.Fatalf("unready workers not replaced: %d", len(cloud.calls))
	}
}
func TestColdBootTimeoutFreesHardCap(t *testing.T) {
	cloud := &fakeCloud{}
	o, _ := NewController(testConfig(t), cloud, &fakeProbe{})
	defer o.Stop()
	o.Reconcile(context.Background())
	o.now = func() time.Time { return time.Now().Add(20 * time.Minute) }
	o.Reconcile(context.Background())
	if len(cloud.terminated) != 1 {
		t.Fatal("failed bootstrap was retained forever")
	}
}

func TestCapacityRejectionHTTP500IsDefinite(t *testing.T) {
	capacity := awserr.NewRequestFailure(awserr.New("InsufficientInstanceCapacity", "no capacity", nil), 500, "id")
	if !definiteLaunchFailure(capacity) {
		t.Fatal("capacity refusal must permit another pool")
	}
	ambiguous := awserr.NewRequestFailure(awserr.New("InternalError", "unknown outcome", nil), 500, "id")
	if definiteLaunchFailure(ambiguous) {
		t.Fatal("generic 500 must retain idempotent launch")
	}
}

func TestRebalanceDoesNotForceCloseHealthyLongSession(t *testing.T) {
	cloud := &fakeCloud{}
	probe := &fakeProbe{ready: true, active: 1}
	o, _ := NewController(testConfig(t), cloud, probe)
	defer o.Stop()
	for range 3 {
		o.Reconcile(context.Background())
	}
	id := cloud.workers[0].InstanceID
	region := cloud.workers[0].Region
	o.Signal(id, region, "EC2 Instance Rebalance Recommendation")
	for range 4 {
		o.Reconcile(context.Background())
	}
	o.now = func() time.Time { return time.Now().Add(time.Hour) }
	o.Reconcile(context.Background())
	if len(cloud.terminated) != 0 {
		t.Fatal("healthy session forcibly terminated on recommendation alone")
	}
	probe.active = 0
	o.Reconcile(context.Background())
	if len(cloud.terminated) != 1 {
		t.Fatal("idle rebalanced worker not retired")
	}
}

func TestPartialDiscoveryKeepsVerifiedWorkersRoutable(t *testing.T) {
	cloud := &fakeCloud{}
	o, _ := NewController(testConfig(t), cloud, &fakeProbe{ready: true})
	defer o.Stop()
	for range 3 {
		o.Reconcile(context.Background())
	}
	cloud.discoverError = errors.New("regional API outage")
	now := time.Now().Add(time.Minute)
	o.now = func() time.Time { return now }
	if o.Reconcile(context.Background()) == nil {
		t.Fatal("missing discovery failure")
	}
	if len(o.Candidates()) != 2 || len(cloud.calls) != 2 || len(cloud.terminated) != 0 {
		t.Fatal("control-plane outage affected verified healthy workers or changed capacity")
	}
}

func TestBothWorkersCanOverlapWithReplacements(t *testing.T) {
	cloud := &fakeCloud{}
	o, _ := NewController(testConfig(t), cloud, &fakeProbe{ready: true, active: 1})
	defer o.Stop()
	for range 3 {
		o.Reconcile(context.Background())
	}
	for _, w := range append([]SpotWorker(nil), cloud.workers...) {
		o.Signal(w.InstanceID, w.Region, "EC2 Instance Rebalance Recommendation")
	}
	for range 4 {
		o.Reconcile(context.Background())
	}
	if len(cloud.calls) != 4 || len(o.Candidates()) != 2 || len(cloud.terminated) != 0 {
		t.Fatal("both workers must be replaceable without killing their healthy active sessions")
	}
}

func TestUnhealthyDrainingWorkerStillRetires(t *testing.T) {
	now := time.Now()
	cfg := testConfig(t)
	cloud := &fakeCloud{workers: []SpotWorker{{InstanceID: "risk", Region: "us-west-2", LaunchedAt: now}, {InstanceID: "healthy1", Region: "us-east-1", LaunchedAt: now}, {InstanceID: "healthy2", Region: "us-west-2", LaunchedAt: now}}}
	probe := &fakeProbe{err: errors.New("worker unreachable")}
	o, err := NewController(cfg, cloud, probe)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Stop()
	o.now = func() time.Time { return now }
	for _, w := range cloud.workers {
		w.State = StateActive
		w.LastHealthy = now
		w.Metadata = WorkerMetadata{Ready: true}
		if w.InstanceID == "risk" {
			w.State = StateDraining
			w.UnhealthySince = now.Add(-time.Hour)
			w.DrainSince = now.Add(-time.Hour)
		}
		o.state.Workers[w.InstanceID] = w
	}
	if err := o.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(cloud.terminated) != 1 || cloud.terminated[0] != "risk" {
		t.Fatalf("retired %v", cloud.terminated)
	}
}
