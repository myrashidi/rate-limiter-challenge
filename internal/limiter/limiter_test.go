package limiter

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// resetLimiterState clears all global state for a clean test environment.
// Go test runner executes tests in the same process, so global sync.Maps persist between tests.
func resetLimiterState() {
	userBuckets = sync.Map{}
	userConfig = sync.Map{}
}

// ------------------------------------------------------------
//  Basic single-user rate limit behavior
// ------------------------------------------------------------

func TestRateLimit_SingleUserBasic(t *testing.T) {
	resetLimiterState()

	user := "userA"
	limit := 5

	// Consume exactly `limit` requests -> all should be allowed
	for i := 0; i < limit; i++ {
		if !RateLimit(user, limit) {
			t.Fatalf("expected request %d to be allowed (limit=%d)", i+1, limit)
		}
	}

	// Next request should be denied immediately
	if RateLimit(user, limit) {
		t.Fatal("expected request to be denied after exceeding limit")
	}

	// Wait ~1/limit sec to allow one token refill
	sleep := time.Duration(1100/limit) * time.Millisecond
	time.Sleep(sleep)

	if !RateLimit(user, limit) {
		t.Fatal("expected request to be allowed after refill")
	}
}

// ------------------------------------------------------------
//  Multiple users should have independent buckets
// ------------------------------------------------------------

func TestRateLimit_MultipleUsersIndependent(t *testing.T) {
	resetLimiterState()

	u1 := "alice"
	u2 := "bob"
	limit := 2

	// Each user can make `limit` requests independently
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

// ------------------------------------------------------------
//  Concurrency: multiple goroutines calling same user
// ------------------------------------------------------------

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

// ------------------------------------------------------------
//  Dynamic configuration: runtime SetUserLimit() override
// ------------------------------------------------------------

func TestRateLimit_UsesConfiguredLimit(t *testing.T) {
	resetLimiterState()

	user := "alice"
	SetUserLimit(user, 1)

	// First request should be allowed
	if !RateLimit(user, 100) { // passed 100 but config overrides to 1
		t.Fatal("expected first request allowed")
	}

	// Second request should be denied (limit = 1)
	if RateLimit(user, 100) {
		t.Fatal("expected second request denied due to config override")
	}
}

// ------------------------------------------------------------
//  Config file: LoadUserConfigFromJSON
// ------------------------------------------------------------

func TestLoadUserConfigFromJSON(t *testing.T) {
	resetLimiterState()

	// Create a temporary config file for this test
	tmpFile := "test_users.json"
	configJSON := `{
		"alice": 2,
		"bob": 4
	}`
	if err := os.WriteFile(tmpFile, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	defer os.Remove(tmpFile)

	// Load config from file
	if err := LoadUserConfigFromJSON(tmpFile); err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Validate loaded limits
	if limit, ok := GetUserLimit("alice"); !ok || limit != 2 {
		t.Fatalf("expected alice=2, got %v (ok=%v)", limit, ok)
	}
	if limit, ok := GetUserLimit("bob"); !ok || limit != 4 {
		t.Fatalf("expected bob=4, got %v (ok=%v)", limit, ok)
	}

	// Ensure RateLimit respects loaded config
	user := "bob"
	for i := 0; i < 4; i++ {
		if !RateLimit(user, 100) {
			t.Fatalf("expected request %d allowed (limit=4)", i+1)
		}
	}
	if RateLimit(user, 100) {
		t.Fatal("expected request denied after exceeding limit")
	}
}
