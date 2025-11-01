package limiter

import (
	"sync"
	"time"
)

// rateLimiter represents a simple token bucket for one user.
type rateLimiter struct {
	mu        sync.Mutex
	capacity  float64   // Maximum number of tokens (burst size)
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

// userBuckets stores a rateLimiter per user safely across goroutines.
var userBuckets sync.Map // map[string]*rateLimiter

// RateLimit limits the number of requests per second per user in a single instance.
//
// It creates or updates a token bucket for each user identified by userID.
// The `limit` argument defines the number of allowed requests per second for that user.
//
// Returns true if the request is allowed, false otherwise.
func RateLimit(userID string, limit int) bool {
	if limit <= 0 {
		return false
	}

	// Try to load an existing limiter for the user
	val, ok := userBuckets.Load(userID)
	if !ok {
		// Create a new limiter if not found
		newRL := newRateLimiter(limit)
		val, _ = userBuckets.LoadOrStore(userID, newRL)
	}

	rl := val.(*rateLimiter)

	// If the rate limit changes, adjust capacity/rate dynamically.
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
