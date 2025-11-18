package limiter

import (
	"strconv"
	"sync"
	"testing"
)

// In-memory sliding benchmarks
func BenchmarkRateLimit_SingleUser(b *testing.B) {
	resetLimiterState()
	SetMode("sliding")
	user := "bench-user"
	limit := 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RateLimit(user, limit)
	}
}

func BenchmarkRateLimit_MultiUserConcurrent(b *testing.B) {
	resetLimiterState()
	SetMode("sliding")
	numUsers := 100
	limit := 50
	users := make([]string, numUsers)
	for i := 0; i < numUsers; i++ {
		users[i] = "user-" + strconv.Itoa(i)
	}

	var wg sync.WaitGroup
	b.ResetTimer()
	for g := 0; g < b.N; g++ {
		for _, u := range users {
			wg.Add(1)
			go func(user string) {
				defer wg.Done()
				_ = RateLimit(user, limit)
			}(u)
		}
	}
	wg.Wait()
}

func BenchmarkRateLimit_ConcurrentSingleUser(b *testing.B) {
	resetLimiterState()
	SetMode("sliding")
	user := "hot-user"
	limit := 1000
	concurrency := 50
	opsPerGoroutine := b.N / concurrency

	var wg sync.WaitGroup
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

// Leaky-bucket benchmark (in-memory)
func BenchmarkRateLimit_LeakyBucket(b *testing.B) {
	resetLimiterState()
	SetMode("leaky")
	user := "leaky-bench-user"
	limit := 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RateLimit(user, limit)
	}
}
