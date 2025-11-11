package limiter

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// resetLimiterState clears all global state for clean test environment
func resetLimiterState() {
	userBuckets = sync.Map{}
	userConfig = sync.Map{}
}

// ----------------------------
// Test: single-user sliding window
// ----------------------------
func TestRateLimit_SingleUserSlidingWindow(t *testing.T) {
	resetLimiterState()
	user := "userA"
	limit := 3

	// Send exactly `limit` requests → all allowed
	for i := 1; i <= limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	// Next request → should be denied
	if RateLimit(user, limit) {
		t.Fatal("request exceeding limit should be denied")
	}

	// Wait >1s to slide window
	time.Sleep(1100 * time.Millisecond)

	// Now requests can pass again
	if !RateLimit(user, limit) {
		t.Fatal("request after window slides should be allowed")
	}
}

// ----------------------------
// Test: multiple users independent
// ----------------------------
func TestRateLimit_MultipleUsersIndependent(t *testing.T) {
	resetLimiterState()
	u1 := "alice"
	u2 := "bob"
	limit := 2

	for i := 1; i <= limit; i++ {
		if !RateLimit(u1, limit) {
			t.Fatalf("%s request %d should be allowed", u1, i)
		}
		if !RateLimit(u2, limit) {
			t.Fatalf("%s request %d should be allowed", u2, i)
		}
	}

	// Next requests → denied
	if RateLimit(u1, limit) {
		t.Fatalf("%s request exceeding limit should be denied", u1)
	}
	if RateLimit(u2, limit) {
		t.Fatalf("%s request exceeding limit should be denied", u2)
	}
}

// ----------------------------
// Test: sliding window precision
// ----------------------------
func TestRateLimit_SlidingWindowPrecision(t *testing.T) {
	resetLimiterState()
	user := "precise-user"
	limit := 3

	// Send exactly `limit` requests
	for i := 1; i <= limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	// Next request → denied
	if RateLimit(user, limit) {
		t.Fatal("request exceeding limit should be denied")
	}

	// Wait >1s for oldest request to slide
	time.Sleep(1100 * time.Millisecond)

	// Now one request should be allowed
	for i := 1; i <= limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatal("request after window slides should be allowed")
		}
	}

	// Next request → denied until next slide
	if RateLimit(user, limit) {
		t.Fatal("next request should still be denied until window slides again")
	}
}

// ----------------------------
// Test: dynamic per-user configuration
// ----------------------------
func TestRateLimit_UsesConfiguredLimit(t *testing.T) {
	resetLimiterState()
	user := "alice"
	SetUserLimit(user, 2) // override default limit

	// First 2 requests → allowed
	if !RateLimit(user, 100) {
		t.Fatal("first request should be allowed (config override)")
	}
	if !RateLimit(user, 100) {
		t.Fatal("second request should be allowed (config override)")
	}

	// Third request → denied
	if RateLimit(user, 100) {
		t.Fatal("third request should be denied (config override)")
	}
}

// ----------------------------
// Test: load config from JSON file
// ----------------------------
func TestLoadUserConfigFromJSON(t *testing.T) {
	resetLimiterState()
	tmpFile := "test_users.json"
	configJSON := `{
		"alice": 2,
		"bob": 4
	}`
	if err := os.WriteFile(tmpFile, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	defer os.Remove(tmpFile)

	if err := LoadUserConfigFromJSON(tmpFile); err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Test alice limit = 2
	user := "alice"
	for i := 1; i <= 2; i++ {
		if !RateLimit(user, 100) {
			t.Fatalf("alice request %d should be allowed", i)
		}
	}
	if RateLimit(user, 100) {
		t.Fatal("alice third request should be denied")
	}

	// Test bob limit = 4
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

// ----------------------------
// Test: concurrency for single user
// ----------------------------
func TestRateLimit_ConcurrentSingleUser(t *testing.T) {
	resetLimiterState()
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

	if allowed > int32(limit) {
		t.Fatalf("allowed more requests (%d) than limit (%d)", allowed, limit)
	}
	if allowed == 0 {
		t.Fatalf("expected at least 1 allowed request, got 0")
	}
}

// ----------------------------
// Test: multi-user concurrency
// ----------------------------
func TestRateLimit_MultiUserConcurrent(t *testing.T) {
	resetLimiterState()
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
