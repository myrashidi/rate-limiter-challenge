package limiter

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// NOTE:
// These tests assume there is a function with signature:
//   func RateLimit(userId string, limit int) bool
// implemented in the same package (limiter).
//
// We'll implement RateLimit in limiter.go next. For now, these tests
// drive the intended behavior for single-instance per-user rate limiting,
// where `limit` is requests-per-second for that user.

func TestRateLimit_SingleUserBasic(t *testing.T) {
	user := "userA"
	limit := 5

	// consume exactly `limit` requests -> all should be allowed
	for i := range limit {
		if !RateLimit(user, limit) {
			t.Fatalf("expected request %d to be allowed (limit=%d)", i+1, limit)
		}
	}

	// next request should be denied immediately
	if RateLimit(user, limit) {
		t.Fatal("expected request to be denied after exceeding limit")
	}

	// wait for one token to refill: 1/limit seconds (plus small epsilon)
	sleep := time.Duration(1100/limit) * time.Millisecond // conservative
	time.Sleep(sleep)

	if !RateLimit(user, limit) {
		t.Fatal("expected request to be allowed after refill")
	}
}

func TestRateLimit_MultipleUsersIndependent(t *testing.T) {
	u1 := "alice"
	u2 := "bob"
	limit := 2

	// Each user should be able to make `limit` requests independently
	for i := 0; i < limit; i++ {
		if !RateLimit(u1, limit) {
			t.Fatalf("expected %s request %d allowed", u1, i+1)
		}
		if !RateLimit(u2, limit) {
			t.Fatalf("expected %s request %d allowed", u2, i+1)
		}
	}

	// Next requests for both should be denied
	if RateLimit(u1, limit) {
		t.Fatalf("expected %s to be rate limited", u1)
	}
	if RateLimit(u2, limit) {
		t.Fatalf("expected %s to be rate limited", u2)
	}
}

func TestRateLimit_ConcurrentSingleUser(t *testing.T) {
	user := "concurrent-user"
	limit := 10

	// attempt 100 concurrent requests; only up to `limit` should be allowed
	var allowed int32
	var wg sync.WaitGroup
	tryCount := 100

	wg.Add(tryCount)
	for i := 0; i < tryCount; i++ {
		go func() {
			defer wg.Done()
			if RateLimit(user, limit) {
				atomic.AddInt32(&allowed, 1)
			}
		}()
	}
	wg.Wait()

	if allowed > int32(limit) {
		t.Fatalf("allowed more requests (%d) than limit (%d)", allowed, limit)
	}
	// allow some slack: we expect between 0 and limit allowed; at least 1 should be allowed
	if allowed == 0 {
		t.Fatalf("expected at least 1 allowed request, got 0")
	}
}

func TestRateLimit_DynamicLimitIncrease(t *testing.T) {
	user := "dynamic-user"
	limit1 := 1
	limit2 := 4

	// With limit1, only one request should be allowed initially
	if !RateLimit(user, limit1) {
		t.Fatal("first request (limit1) should be allowed")
	}
	if RateLimit(user, limit1) {
		t.Fatal("second request (limit1) should be denied")
	}

	// After increasing limit (passing larger `limit2`), we should be allowed more requests.
	// Sleep a small amount to allow any internal token accounting to proceed.
	time.Sleep(300 * time.Millisecond)

	allowedCount := 0
	for i := 0; i < limit2; i++ {
		if RateLimit(user, limit2) {
			allowedCount++
		}
	}
	if allowedCount == 0 {
		t.Fatalf("expected at least some allowed requests after increasing limit to %d", limit2)
	}
}
