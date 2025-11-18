package limiter

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func resetLimiterState() {
	// reset maps used by package
	userBuckets = sync.Map{}
	userSlices = sync.Map{}
	userConfig = sync.Map{}
	leakyBuckets = sync.Map{}
	// default mode
	SetMode("sliding")
	// disable redis by default in unit tests
	rdb = nil
}

// ----------------------------
// Sliding-window (in-memory) tests
// ----------------------------
func TestRateLimit_SingleUserSlidingWindow(t *testing.T) {
	resetLimiterState()
	SetMode("sliding")

	user := "userA"
	limit := 3

	for i := 1; i <= limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	if RateLimit(user, limit) {
		t.Fatal("request exceeding limit should be denied")
	}

	time.Sleep(1100 * time.Millisecond)
	// after >1s, window cleared
	for i := 1; i <= limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("after window slide, request %d should be allowed", i)
		}
	}
}

func TestRateLimit_MultipleUsersIndependent(t *testing.T) {
	resetLimiterState()
	SetMode("sliding")

	u1, u2 := "alice", "bob"
	limit := 2

	for i := 1; i <= limit; i++ {
		if !RateLimit(u1, limit) {
			t.Fatalf("%s request %d should be allowed", u1, i)
		}
		if !RateLimit(u2, limit) {
			t.Fatalf("%s request %d should be allowed", u2, i)
		}
	}

	if RateLimit(u1, limit) || RateLimit(u2, limit) {
		t.Fatal("requests exceeding limit should be denied")
	}
}

func TestRateLimit_SlidingWindowPrecision(t *testing.T) {
	resetLimiterState()
	SetMode("sliding")

	user := "precise-user"
	limit := 3

	for i := 1; i <= limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if RateLimit(user, limit) {
		t.Fatal("request exceeding limit should be denied")
	}

	time.Sleep(1100 * time.Millisecond)
	// Now limit requests allowed again
	for i := 1; i <= limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("after sliding window, request %d should be allowed", i)
		}
	}
	if RateLimit(user, limit) {
		t.Fatal("after consuming limit again, next should be denied")
	}
}

func TestRateLimit_UsesConfiguredLimit(t *testing.T) {
	resetLimiterState()
	SetMode("sliding")

	user := "alice"
	SetUserLimit(user, 2)

	if !RateLimit(user, 100) || !RateLimit(user, 100) {
		t.Fatal("first two requests should be allowed")
	}
	if RateLimit(user, 100) {
		t.Fatal("third request should be denied")
	}
}

func TestLoadUserConfigFromJSON(t *testing.T) {
	resetLimiterState()
	SetMode("sliding")

	tmpFile := "test_users.json"
	configJSON := `{"alice":2,"bob":4}`
	if err := os.WriteFile(tmpFile, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write tmp config: %v", err)
	}
	defer os.Remove(tmpFile)

	if err := LoadUserConfigFromJSON(tmpFile); err != nil {
		t.Fatal(err)
	}

	user := "alice"
	for i := 1; i <= 2; i++ {
		if !RateLimit(user, 100) {
			t.Fatalf("alice request %d should be allowed", i)
		}
	}
	if RateLimit(user, 100) {
		t.Fatal("alice third request should be denied")
	}

	user = "bob"
	for i := 1; i <= 4; i++ {
		if !RateLimit(user, 100) {
			t.Fatalf("bob request %d should be allowed", i)
		}
	}
	if RateLimit(user, 100) {
		t.Fatal("bob fifth request should be denied")
	}
}

func TestRateLimit_ConcurrentSingleUser(t *testing.T) {
	resetLimiterState()
	SetMode("sliding")

	user := "concurrent-user"
	limit := 10
	tryCount := 100

	var allowed int32
	var wg sync.WaitGroup
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
	if allowed > int32(limit) || allowed == 0 {
		t.Fatalf("unexpected allowed requests: %d", allowed)
	}
}

func TestRateLimit_MultiUserConcurrent(t *testing.T) {
	resetLimiterState()
	SetMode("sliding")

	numUsers := 50
	limit := 5
	users := make([]string, numUsers)
	for i := 0; i < numUsers; i++ {
		users[i] = "user-" + strconv.Itoa(i)
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

// ----------------------------
// Leaky-bucket (in-memory) tests
// ----------------------------
func TestRateLimit_LeakyBucketBasic(t *testing.T) {
	resetLimiterState()
	SetMode("leaky")

	user := "leaky-user"
	limit := 3 // capacity and leak rate

	// first 3 requests should be allowed (bucket initially full)
	for i := 0; i < limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("leaky request %d should be allowed", i+1)
		}
	}
	// next one should be denied
	if RateLimit(user, limit) {
		t.Fatal("leaky: next request should be denied after capacity used")
	}

	// wait enough time to refill one token (~1/limit seconds -> but we use ms)
	time.Sleep(350 * time.Millisecond) // ~0.35s refill ~1.05 tokens for limit=3
	if !RateLimit(user, limit) {
		t.Fatal("leaky: request after partial refill should be allowed")
	}
}

func TestRateLimit_LeakyBucketConcurrent(t *testing.T) {
	resetLimiterState()
	SetMode("leaky")

	user := "leaky-concurrent"
	limit := 10
	tryCount := 100

	var allowed int32
	var wg sync.WaitGroup
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
	if allowed > int32(limit) || allowed == 0 {
		t.Fatalf("leaky concurrent: unexpected allowed requests: %d", allowed)
	}
}
