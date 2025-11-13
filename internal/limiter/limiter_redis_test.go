package limiter

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRateLimitRedis_ConcurrentSingleUser
func TestRateLimitRedis_ConcurrentSingleUser(t *testing.T) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		t.Skip("redis not available")
	}
	// ensure clean DB
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("failed to flush redis DB: %v", err)
	}

	user := "redis-concurrent"
	limit := 10
	const goroutines = 100

	var allowed int32
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if RateLimit(user, limit) {
				atomic.AddInt32(&allowed, 1)
			}
		}()
	}
	wg.Wait()
	if allowed > int32(limit) {
		t.Fatalf("expected <= %d allowed, got %d", limit, allowed)
	}
}

// TestRateLimitRedis_SameMillisecondBurst
func TestRateLimitRedis_SameMillisecondBurst(t *testing.T) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		t.Skip("redis not available")
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("failed to flush redis DB: %v", err)
	}

	user := "redis-same-ms"
	limit := 3

	for i := 0; i < limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if RateLimit(user, limit) {
		t.Fatal("next request should be denied")
	}
}

// TestRateLimitRedis_MultiUserParallel
func TestRateLimitRedis_MultiUserParallel(t *testing.T) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		t.Skip("redis not available")
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("failed to flush redis DB: %v", err)
	}

	numUsers := 30
	limit := 5
	users := make([]string, numUsers)
	for i := 0; i < numUsers; i++ {
		users[i] = "redis-user-" + strconv.Itoa(i)
	}

	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func(user string) {
			defer wg.Done()
			for i := 0; i < limit; i++ {
				if !RateLimit(user, limit) {
					t.Fatalf("%s request %d should be allowed", user, i+1)
				}
			}
			if RateLimit(user, limit) {
				t.Fatalf("%s request exceeding limit should be denied", user)
			}
		}(u)
	}
	wg.Wait()
}

// TestRateLimitRedis_WindowExpiry
func TestRateLimitRedis_WindowExpiry(t *testing.T) {
	InitRedis("localhost:6379", "", 0)
	if rdb == nil {
		t.Skip("redis not available")
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("failed to flush redis DB: %v", err)
	}

	user := "redis-expiry"
	limit := 3
	for i := 0; i < limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if RateLimit(user, limit) {
		t.Fatal("request exceeding limit should be denied")
	}

	time.Sleep(1100 * time.Millisecond)
	for i := 0; i < limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("after window clears, request %d should be allowed", i+1)
		}
	}
}
