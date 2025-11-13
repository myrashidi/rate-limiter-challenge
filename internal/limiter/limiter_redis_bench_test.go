package limiter

import (
	"strconv"
	"sync"
	"testing"
)

// ----------------------------
// Benchmark: single user sequential
// ----------------------------
func BenchmarkRateLimitRedis_SingleUser(b *testing.B) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		b.Skip("redis not available")
	}
	_ = rdb.FlushDB(ctx).Err()

	user := "bench-redis-single"
	limit := 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RateLimit(user, limit)
	}
}

// ----------------------------
// Benchmark: high concurrency on single hot user
// ----------------------------
func BenchmarkRateLimitRedis_ConcurrentHotUser(b *testing.B) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		b.Skip("redis not available")
	}
	_ = rdb.FlushDB(ctx).Err()

	user := "bench-redis-hot"
	limit := 100

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
// Benchmark: many concurrent users
// ----------------------------
func BenchmarkRateLimitRedis_ManyUsersConcurrent(b *testing.B) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		b.Skip("redis not available")
	}
	_ = rdb.FlushDB(ctx).Err()

	numUsers := 200
	limit := 20
	users := make([]string, numUsers)
	for i := 0; i < numUsers; i++ {
		users[i] = "bench-redis-user-" + strconv.Itoa(i)
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
