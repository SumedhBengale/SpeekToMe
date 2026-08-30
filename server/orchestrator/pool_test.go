package orchestrator

import (
	"context"
	"testing"
	"time"

	"speektome-server/config"
)

func TestOrchestratorReconciliationLifecycle(t *testing.T) {
	mockSM := &MockSpotManager{}
	orch := NewOrchestrator(mockSM, "ami-test", "key-test", "sg-test", "")
	orch.SetMaxLatency(2 * time.Second)

	ctx := context.Background()

	// 1. Initially disabled: should do nothing
	config.ConfigManager.Set(config.ProviderConfig{
		InferenceInstanceEnabled: false,
		Models:                   config.DefaultModelsConfig(),
	}, nil)

	orch.Reconcile(ctx)
	status := orch.GetStatus()
	if status["active_worker"] != nil {
		t.Fatalf("expected active_worker to be nil when disabled, got %v", status["active_worker"])
	}

	// 2. Enable inference instances: Reconcile should provision
	config.ConfigManager.Set(config.ProviderConfig{
		InferenceInstanceEnabled: true,
		Models:                   config.DefaultModelsConfig(),
	}, nil)

	orch.Reconcile(ctx)
	status = orch.GetStatus()
	worker, ok := status["active_worker"].(*SpotWorker)
	if !ok || worker == nil {
		t.Fatalf("expected active_worker to be provisioned, got %v", status["active_worker"])
	}
	if worker.State != StateProvisioning {
		t.Fatalf("expected state PROVISIONING, got %s", worker.State)
	}

	// 3. Worker sends heartbeat: transitions to ACTIVE
	err := orch.RegisterHeartbeat(worker.InstanceID, worker.Region, "10.0.0.5")
	if err != nil {
		t.Fatalf("unexpected error registering heartbeat: %v", err)
	}
	status = orch.GetStatus()
	worker = status["active_worker"].(*SpotWorker)
	if worker.State != StateActive {
		t.Fatalf("expected state ACTIVE, got %s", worker.State)
	}
	if worker.IPAddress != "10.0.0.5" {
		t.Fatalf("expected IP 10.0.0.5, got %s", worker.IPAddress)
	}

	// Verify dynamic config update
	currConfig := config.ConfigManager.Get()
	selfHosted := currConfig.Models.StreamingProviders["self_hosted_nemotron_3_5"]
	if selfHosted.BaseURL != "ws://10.0.0.5:8080/v1/audio/stream" {
		t.Fatalf("expected dynamic BaseURL updated to ws://10.0.0.5:8080/v1/audio/stream, got %s", selfHosted.BaseURL)
	}

	// 4. Disable inference instances: Reconcile should scale down to 0
	config.ConfigManager.Set(config.ProviderConfig{
		InferenceInstanceEnabled: false,
		Models:                   config.DefaultModelsConfig(),
	}, nil)

	orch.Reconcile(ctx)
	status = orch.GetStatus()
	if status["active_worker"] != nil {
		t.Fatalf("expected active_worker to be nil after disabling, got %v", status["active_worker"])
	}
}
