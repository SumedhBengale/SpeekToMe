package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestGetValidLowLatencyRegions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	candidates := []string{"us-east-1", "us-east-2"}
	valid := GetValidLowLatencyRegions(ctx, candidates, 500*time.Millisecond)

	if len(valid) == 0 {
		t.Logf("Warning: No regional endpoints reachable in test environment")
	} else {
		for _, v := range valid {
			t.Logf("Region %s reachable: latency = %.2f ms", v.Region, v.LatencyMs)
		}
	}
}
