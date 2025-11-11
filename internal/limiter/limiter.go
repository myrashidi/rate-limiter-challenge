package limiter

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// ----------------------------
// Sliding-window limiter per user
// ----------------------------

type rateLimiter struct {
	mu       sync.Mutex
	requests []time.Time
	capacity int
}

// newRateLimiter creates a limiter with a max number of requests per second.
func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{
		requests: make([]time.Time, 0, limit),
		capacity: limit,
	}
}

// allow checks if a request is allowed, updates timestamps slice
func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Second)

	// Remove timestamps older than 1 second
	i := 0
	for ; i < len(rl.requests); i++ {
		if rl.requests[i].After(cutoff) {
			break
		}
	}
	rl.requests = rl.requests[i:]

	if len(rl.requests) < rl.capacity {
		rl.requests = append(rl.requests, now)
		return true
	}
	return false
}

// ----------------------------
// Global maps
// ----------------------------

var userBuckets sync.Map // map[string]*rateLimiter
var userConfig sync.Map  // map[string]int

// ----------------------------
// Configuration management
// ----------------------------

func SetUserLimit(userID string, limit int) {
	userConfig.Store(userID, limit)
}

func GetUserLimit(userID string) (int, bool) {
	val, ok := userConfig.Load(userID)
	if !ok {
		return 0, false
	}
	return val.(int), true
}

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

// ----------------------------
// Main API
// ----------------------------

func RateLimit(userID string, limit int) bool {
	if limit <= 0 {
		return false
	}

	// Override if user has custom config
	if custom, ok := GetUserLimit(userID); ok {
		limit = custom
	}

	val, ok := userBuckets.Load(userID)
	if !ok {
		newRL := newRateLimiter(limit)
		val, _ = userBuckets.LoadOrStore(userID, newRL)
	}
	rl := val.(*rateLimiter)

	// Update capacity if dynamic limit changed
	rl.mu.Lock()
	if rl.capacity != limit {
		rl.capacity = limit
		if cap(rl.requests) < limit {
			newRequests := make([]time.Time, len(rl.requests), limit)
			copy(newRequests, rl.requests)
			rl.requests = newRequests
		}
	}
	rl.mu.Unlock()

	return rl.allow()
}
