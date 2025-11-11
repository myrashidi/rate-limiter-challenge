package limiter

import (
	"strconv"
	"sync"
	"testing"
)

// ----------------------------
// Benchmark: single-user sequential requests
// ----------------------------
func BenchmarkRateLimit_SingleUser(b *testing.B) {
	resetLimiterState()
	user := "bench-user"
	limit := 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RateLimit(user, limit)
	}
}

// ----------------------------
// Benchmark: multi-user concurrent requests
// ----------------------------
func BenchmarkRateLimit_MultiUserConcurrent(b *testing.B) {
	resetLimiterState()
	numUsers := 100
	users := make([]string, numUsers)
	for i := 0; i < numUsers; i++ {
		users[i] = "user-" + strconv.Itoa(i)
	}

	limit := 50

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			user := users[i%numUsers]
			_ = RateLimit(user, limit)
			i++
		}
	})
}

// ----------------------------
// Benchmark: high concurrency single user
// ----------------------------
func BenchmarkRateLimit_ConcurrentSingleUser(b *testing.B) {
	resetLimiterState()
	user := "hot-user"
	limit := 1000

	var wg sync.WaitGroup
	concurrency := 50
	opsPerGoroutine := b.N / concurrency

	b.ResetTimer()
	wg.Add(concurrency)
	for g := 0; g < concurrency; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				_ = RateLimit(user, limit)
			}
		}()
	}
	wg.Wait()
}

// ----------------------------
// Benchmark: sliding window precision under concurrency
// Simulate 3 requests per second for a hot user
// ----------------------------
func BenchmarkRateLimit_SlidingWindowHotUser(b *testing.B) {
	resetLimiterState()
	user := "hot-user"
	limit := 3

	var wg sync.WaitGroup
	concurrency := 10
	opsPerGoroutine := b.N / concurrency

	wg.Add(concurrency)
	b.ResetTimer()
	for g := 0; g < concurrency; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				_ = RateLimit(user, limit)
			}
		}()
	}
	wg.Wait()
}
