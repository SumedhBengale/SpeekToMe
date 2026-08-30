package orchestrator

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type MockSpotManager struct{}

func (m *MockSpotManager) FindCheapestValidPool(ctx context.Context, validRegions []RegionLatency, instanceTypes []string) (*SpotPoolCandidate, error) {
	return &SpotPoolCandidate{
		Region:           "us-east-2",
		AvailabilityZone: "us-east-2a",
		InstanceType:     "g6f.large",
		SpotPriceUSD:     0.22,
		LatencyMs:        25.0,
	}, nil
}

func (m *MockSpotManager) LaunchSpotWorker(ctx context.Context, pool *SpotPoolCandidate, amiID string, keyName string, securityGroup string, userData string) (string, error) {
	return "i-0123456789abcdef0", nil
}

func (m *MockSpotManager) TerminateWorker(ctx context.Context, region string, instanceID string) error {
	return nil
}

func (m *MockSpotManager) DescribeWorkerIP(ctx context.Context, region string, instanceID string) (string, error) {
	return "192.168.1.100", nil
}

func TestHandleSpotEvent(t *testing.T) {
	orch := NewOrchestrator(&MockSpotManager{}, "ami-test", "", "", "")

	eventJSON := `{
		"version": "0",
		"id": "12345678-1234-1234-1234-123456789012",
		"detail-type": "EC2 Spot Instance Interruption Warning",
		"source": "aws.ec2",
		"account": "123456789012",
		"time": "2026-08-28T22:00:00Z",
		"region": "us-east-2",
		"resources": ["arn:aws:ec2:us-east-2:123456789012:instance/i-0123456789abcdef0"],
		"detail": {
			"instance-id": "i-0123456789abcdef0",
			"instance-action": "terminate"
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhooks/aws-spot-event", bytes.NewBufferString(eventJSON))
	rec := httptest.NewRecorder()

	orch.HandleSpotEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
