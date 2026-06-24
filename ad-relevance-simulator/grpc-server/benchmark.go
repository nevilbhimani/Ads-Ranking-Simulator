// benchmark.go — Load benchmarking for P99 latency measurement
//
// Build: go build -o benchmark ./benchmark.go
// Run:   ./benchmark --rps=500 --duration=30s --addr=localhost:8081
//
// WHY measure P99 and not average?
//   Average latency hides tail behavior. If 99% of requests complete in 5ms
//   but 1% take 500ms (e.g., cache misses hitting a slow DB), the average
//   might show 10ms — misleadingly good. P99 captures what your slowest
//   users experience. At 500 RPS, P99 means 5 users per second see that
//   worst-case latency. Ad systems care deeply about this because slow
//   ranking decisions can miss the auction deadline (typical: 100ms total
//   budget for the entire ad stack).
//
// WHY use a token bucket for RPS control?
//   A naive approach (goroutine-per-request with a sleep) can't accurately
//   control rate because sleep precision is ~1ms. A token bucket is the
//   standard rate-limiting algorithm: you get 1/RPS tokens per second,
//   and each request consumes one token. If tokens are exhausted, you wait.
//   This produces accurate RPS even at high rates.

//go:build ignore

package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	rps := flag.Int("rps", 500, "Target requests per second")
	duration := flag.Duration("duration", 30*time.Second, "Benchmark duration")
	addr := flag.String("addr", "localhost:8081", "Server address (HTTP shim)")
	flag.Parse()

	url := fmt.Sprintf("http://%s/rank?top_n=10&campaign_id=0", *addr)
	interval := time.Duration(float64(time.Second) / float64(*rps))

	fmt.Printf("Benchmarking %s\n", url)
	fmt.Printf("Target: %d RPS for %s\n", *rps, *duration)
	fmt.Printf("Request interval: %v\n\n", interval)

	var (
		totalRequests int64
		cacheHits     int64
		errors        int64
		latencies     []int64
		mu            sync.Mutex
	)

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 100,
			// Disable keep-alive reuse limit — we want persistent connections
			// to measure server latency, not TCP handshake cost
			DisableKeepAlives: false,
		},
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup

	for time.Now().Before(deadline) {
		<-ticker.C

		wg.Add(1)
		go func() {
			defer wg.Done()

			start := time.Now()
			resp, err := client.Get(url)
			latencyUs := time.Since(start).Microseconds()

			atomic.AddInt64(&totalRequests, 1)

			if err != nil {
				atomic.AddInt64(&errors, 1)
				return
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body) // drain body

			if resp.Header.Get("X-Cache") == "HIT" {
				atomic.AddInt64(&cacheHits, 1)
			}

			mu.Lock()
			latencies = append(latencies, latencyUs)
			mu.Unlock()
		}()
	}

	wg.Wait()

	// ── Statistics ──────────────────────────────────────────────────────────
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	n := len(latencies)

	percentile := func(p float64) float64 {
		if n == 0 {
			return 0
		}
		idx := int(math.Ceil(p/100.0*float64(n))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return float64(latencies[idx]) / 1000.0 // convert µs → ms
	}

	var sum int64
	for _, l := range latencies {
		sum += l
	}
	avgMs := float64(sum) / float64(n) / 1000.0

	actualRPS := float64(totalRequests) / duration.Seconds()

	fmt.Println("═══════════════════════════════════")
	fmt.Println("         BENCHMARK RESULTS         ")
	fmt.Println("═══════════════════════════════════")
	fmt.Printf("Total requests:    %d\n", totalRequests)
	fmt.Printf("Errors:            %d (%.2f%%)\n", errors, float64(errors)/float64(totalRequests)*100)
	fmt.Printf("Actual RPS:        %.1f\n", actualRPS)
	fmt.Println()
	fmt.Printf("Latency (ms):\n")
	fmt.Printf("  P50 (median):    %.2f ms\n", percentile(50))
	fmt.Printf("  P90:             %.2f ms\n", percentile(90))
	fmt.Printf("  P95:             %.2f ms\n", percentile(95))
	fmt.Printf("  P99:             %.2f ms\n", percentile(99))
	fmt.Printf("  P99.9:           %.2f ms\n", percentile(99.9))
	fmt.Printf("  Max:             %.2f ms\n", percentile(100))
	fmt.Printf("  Average:         %.2f ms\n", avgMs)
	fmt.Println()

	p99 := percentile(99)
	if p99 < 20 {
		fmt.Printf("✓ P99 %.2fms < 20ms target — PASSED\n", p99)
	} else {
		fmt.Printf("✗ P99 %.2fms ≥ 20ms target — cache TTL or pool config needs tuning\n", p99)
	}

	// Cache effectiveness
	fmt.Printf("\nCache hit rate: %.1f%%\n", float64(cacheHits)/float64(totalRequests)*100)
	fmt.Println()
	fmt.Println("Interpretation guide:")
	fmt.Println("  P50: what half your users experience (typical case)")
	fmt.Println("  P99: what 1 in 100 users experience (worst-case budget)")
	fmt.Println("  P99 < 20ms with cache: most requests are in-memory reads")
	fmt.Println("  Cache misses (every 5s) cause the P99 spike vs P50 gap")
}
