package limiter

import (
	"strconv"
	"sync"
	"testing"
)

// ----------------------------
// In-memory benchmarks
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

func BenchmarkRateLimit_MultiUserConcurrent(b *testing.B) {
	resetLimiterState()
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

// ----------------------------
// Redis benchmarks
// ----------------------------
func BenchmarkRateLimitRedis_MultiUserConcurrent(b *testing.B) {
	// ensure redis and start from a clean DB
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		b.Skip("redis not available")
	}
	_ = rdb.FlushDB(ctx).Err()

	numUsers := 50
	limit := 5
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
