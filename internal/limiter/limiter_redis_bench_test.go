package limiter

import (
	"strconv"
	"sync"
	"testing"
)

func BenchmarkRateLimitRedis_SingleUser(b *testing.B) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		b.Skip("redis not available")
	}
	_ = rdb.FlushDB(ctx).Err()

	SetMode("sliding")
	user := "bench-redis-single"
	limit := 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RateLimit(user, limit)
	}
}

func BenchmarkRateLimitRedis_ManyUsers(b *testing.B) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		b.Skip("redis not available")
	}
	_ = rdb.FlushDB(ctx).Err()

	SetMode("sliding")
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
