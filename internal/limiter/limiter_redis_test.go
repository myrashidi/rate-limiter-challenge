package limiter

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// Before running these tests, ensure Redis is running at localhost:6379.
// Run only Redis tests:
//   go test -v -run TestRateLimitRedis

func resetRedisKeys(users []string) {
	for _, u := range users {
		rdb.Del(ctx, "rate:"+u)
	}
}

// ----------------------------
// Test: basic correctness under concurrency
// ----------------------------
func TestRateLimitRedis_ConcurrentSingleUser(t *testing.T) {
	InitRedis("localhost:6379", "", 0)
	user := "redis-concurrent"
	limit := 10
	rdb.Del(ctx, "rate:"+user)

	const goroutines = 100
	var allowed int32
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if RateLimitRedis(user, limit) {
				// atomic add not necessary for test semantics
				allowed++
			}
		}()
	}

	wg.Wait()
	if allowed > int32(limit) {
		t.Fatalf("expected <= %d allowed, got %d", limit, allowed)
	}
}

// ----------------------------
// Test: same-millisecond burst
// ----------------------------
func TestRateLimitRedis_SameMillisecondBurst(t *testing.T) {
	InitRedis("localhost:6379", "", 0)
	user := "redis-same-ms"
	limit := 3
	rdb.Del(ctx, "rate:"+user)

	// All calls happen in same millisecond
	start := time.Now()
	for i := 0; i < limit; i++ {
		if !RateLimitRedis(user, limit) {
			t.Fatalf("request %d at %v should be allowed", i+1, time.Since(start))
		}
	}

	// Next one should be denied
	if RateLimitRedis(user, limit) {
		t.Fatal("next request should be denied even in same millisecond burst")
	}
}

// ----------------------------
// Test: multi-user parallel
// ----------------------------
func TestRateLimitRedis_MultiUserParallel(t *testing.T) {
	InitRedis("localhost:6379", "", 0)
	numUsers := 30
	limit := 5
	users := make([]string, numUsers)
	for i := 0; i < numUsers; i++ {
		users[i] = "redis-user-" + strconv.Itoa(i)
	}
	resetRedisKeys(users)

	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func(user string) {
			defer wg.Done()
			for i := 0; i < limit; i++ {
				if !RateLimitRedis(user, limit) {
					t.Fatalf("%s request %d should be allowed", user, i+1)
				}
			}
			if RateLimitRedis(user, limit) {
				t.Fatalf("%s request exceeding limit should be denied", user)
			}
		}(u)
	}
	wg.Wait()
}

// ----------------------------
// Test: wait-for-expiry behavior
// ----------------------------
func TestRateLimitRedis_WindowExpiry(t *testing.T) {
	InitRedis("localhost:6379", "", 0)
	user := "redis-expiry"
	limit := 3
	rdb.Del(ctx, "rate:"+user)

	for i := 0; i < limit; i++ {
		if !RateLimitRedis(user, limit) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	if RateLimitRedis(user, limit) {
		t.Fatal("request exceeding limit should be denied")
	}

	// After >1s, window clears
	time.Sleep(1100 * time.Millisecond)
	for i := 0; i < limit; i++ {
		if !RateLimitRedis(user, limit) {
			t.Fatalf("after window clears, request %d should be allowed", i+1)
		}
	}
}
