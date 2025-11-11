package limiter

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// ----------------------------------------------------
//  Internal token-bucket structure (per user)
// ----------------------------------------------------

// rateLimiter represents a simple token bucket for one user.
type rateLimiter struct {
	mu        sync.Mutex
	capacity  float64   // maximum number of tokens (burst size)
	tokens    float64   // current available tokens
	rate      float64   // tokens added per second
	lastCheck time.Time // last refill timestamp
}

// newRateLimiter creates a new token bucket with a given rate and capacity.
func newRateLimiter(limit int) *rateLimiter {
	now := time.Now()
	return &rateLimiter{
		capacity:  float64(limit),
		tokens:    float64(limit),
		rate:      float64(limit),
		lastCheck: now,
	}
}

// allow consumes a token if available and returns true; otherwise false.
func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastCheck).Seconds()
	rl.lastCheck = now

	// Refill tokens based on elapsed time
	rl.tokens += elapsed * rl.rate
	if rl.tokens > rl.capacity {
		rl.tokens = rl.capacity
	}

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// ----------------------------------------------------
//  Global structures for all users
// ----------------------------------------------------

// userBuckets stores a rateLimiter per user safely across goroutines.
var userBuckets sync.Map // map[string]*rateLimiter

// userConfig stores dynamic per-user rate limits (requests per second).
// These override the "limit" argument passed to RateLimit if present.
var userConfig sync.Map // map[string]int

// ----------------------------------------------------
//  Configuration management
// ----------------------------------------------------

// SetUserLimit dynamically updates or adds a rate limit for a user.
// Safe for concurrent use.
func SetUserLimit(userID string, limit int) {
	userConfig.Store(userID, limit)
}

// GetUserLimit returns the configured limit for a user, or ok=false if none.
func GetUserLimit(userID string) (int, bool) {
	val, ok := userConfig.Load(userID)
	if !ok {
		return 0, false
	}
	return val.(int), true
}

// LoadUserConfigFromJSON loads user limits from a JSON file.
// Example file:
//
//	{
//	  "alice": 5,
//	  "bob": 10
//	}
func LoadUserConfigFromJSON(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg map[string]int
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	for user, limit := range cfg {
		SetUserLimit(user, limit)
	}
	return nil
}

// ----------------------------------------------------
//  Main API
// ----------------------------------------------------

// RateLimit limits the number of requests per second per user in a single instance.
//
// Parameters:
//   - userID: unique user identifier
//   - limit:  default max requests per second (used if user has no custom config)
//
// Returns true if the request is allowed, false if denied.
func RateLimit(userID string, limit int) bool {
	if limit <= 0 {
		return false
	}

	// Override if a user-specific limit exists
	if custom, ok := GetUserLimit(userID); ok {
		limit = custom
	}

	// Try to load or create a limiter for this user
	val, ok := userBuckets.Load(userID)
	if !ok {
		newRL := newRateLimiter(limit)
		val, _ = userBuckets.LoadOrStore(userID, newRL)
	}
	rl := val.(*rateLimiter)

	// If rate changed (via config), adjust dynamically
	rl.mu.Lock()
	if rl.rate != float64(limit) {
		rl.rate = float64(limit)
		rl.capacity = float64(limit)
		if rl.tokens > rl.capacity {
			rl.tokens = rl.capacity
		}
	}
	rl.mu.Unlock()

	return rl.allow()
}
