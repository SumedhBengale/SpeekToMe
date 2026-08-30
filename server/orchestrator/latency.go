package orchestrator

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// DefaultCandidateRegions lists standard low-latency US and Canadian AWS regions.
var DefaultCandidateRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-2",
	"us-west-1",
	"ca-central-1",
}

// RegionLatency represents a ping result to a specific AWS region.
type RegionLatency struct {
	Region    string        `json:"region"`
	Latency   time.Duration `json:"latency"`
	LatencyMs float64       `json:"latency_ms"`
	Reachable bool          `json:"reachable"`
	Error     string        `json:"error,omitempty"`
}

// MeasureRegionLatency probes an AWS regional endpoint via TCP handshake to measure RTT.
func MeasureRegionLatency(ctx context.Context, region string, timeout time.Duration) RegionLatency {
	endpoint := fmt.Sprintf("ec2.%s.amazonaws.com:443", region)
	dialer := net.Dialer{Timeout: timeout}

	t0 := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", endpoint)
	elapsed := time.Since(t0)

	if err != nil {
		return RegionLatency{
			Region:    region,
			Reachable: false,
			Error:     err.Error(),
		}
	}
	_ = conn.Close()

	return RegionLatency{
		Region:    region,
		Latency:   elapsed,
		LatencyMs: float64(elapsed.Microseconds()) / 1000.0,
		Reachable: true,
	}
}

// GetValidLowLatencyRegions measures latencies across all candidate regions concurrently and returns those below maxLatency.
func GetValidLowLatencyRegions(ctx context.Context, candidateRegions []string, maxLatency time.Duration) []RegionLatency {
	if len(candidateRegions) == 0 {
		candidateRegions = DefaultCandidateRegions
	}

	results := make([]RegionLatency, len(candidateRegions))
	var wg sync.WaitGroup

	for i, r := range candidateRegions {
		wg.Add(1)
		go func(idx int, region string) {
			defer wg.Done()
			results[idx] = MeasureRegionLatency(ctx, region, 2*time.Second)
		}(i, r)
	}

	wg.Wait()

	var valid []RegionLatency
	for _, res := range results {
		if res.Reachable && res.Latency <= maxLatency {
			valid = append(valid, res)
		}
	}

	// Sort lowest latency first
	sort.Slice(valid, func(i, j int) bool {
		return valid[i].Latency < valid[j].Latency
	})

	return valid
}
