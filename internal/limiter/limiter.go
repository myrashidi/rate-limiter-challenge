package limiter

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	// in-memory structures
	userBuckets = sync.Map{} // map[string]*sync.Mutex
	userSlices  = sync.Map{} // map[string]*[]int64
	userConfig  = sync.Map{} // map[string]int

	// redis
	rdb *redis.Client
	ctx = context.Background()
)

// ----------------------------
// Config management
// ----------------------------

// SetUserLimit sets per-user configured limit (requests per second).
func SetUserLimit(userID string, limit int) {
	userConfig.Store(userID, limit)
}

// GetUserLimit returns configured per-user limit.
func GetUserLimit(userID string) (int, bool) {
	v, ok := userConfig.Load(userID)
	if !ok {
		return 0, false
	}
	return v.(int), true
}

// LoadUserConfigFromJSON loads per-user limits from a JSON file.
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
// Redis init
// ----------------------------

func InitRedis(addr string, password string, db int) {
	rdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
}

// ----------------------------
// Internal implementations
// ----------------------------

// rateLimitMemory implements sliding-window in-memory limiter.
func rateLimitMemory(userID string, limit int) bool {
	// get mutex for user
	val, _ := userBuckets.LoadOrStore(userID, &sync.Mutex{})
	mtx := val.(*sync.Mutex)

	// get slice pointer for timestamps
	rawSlice, _ := userSlices.LoadOrStore(userID, &[]int64{})
	tsSlice := rawSlice.(*[]int64)

	now := time.Now().UnixMilli()

	mtx.Lock()
	defer mtx.Unlock()

	// prune timestamps older than 1s
	cutoff := now - 1000
	newSlice := (*tsSlice)[:0] // reuse slice backing
	for _, ts := range *tsSlice {
		if ts > cutoff {
			newSlice = append(newSlice, ts)
		}
	}
	// if capacity changed, we keep newSlice content (we reuse)
	if len(newSlice) >= limit {
		*tsSlice = newSlice
		return false
	}
	// allow request
	newSlice = append(newSlice, now)
	*tsSlice = newSlice
	return true
}

// rateLimitRedis implements distributed sliding-window limiter via Redis.
// It returns true if allowed, false otherwise.
func rateLimitRedis(userID string, limit int) bool {
	if rdb == nil || limit <= 0 {
		return false
	}

	// Use a single timestamp value (ms) as score and unique member (ns) to avoid collisions.
	t := time.Now()
	nowMs := t.UnixMilli()
	nowNs := t.UnixNano()
	oneSecondAgoMs := nowMs - 1000
	key := "rate:" + userID

	// Lua script: remove old by score, count, if < limit add new member (unique), set expire
	const lua = `
		redis.call("ZREMRANGEBYSCORE", KEYS[1], 0, ARGV[1])
		local current = redis.call("ZCARD", KEYS[1])
		if tonumber(current) < tonumber(ARGV[2]) then
			redis.call("ZADD", KEYS[1], ARGV[3], ARGV[4])
			redis.call("PEXPIRE", KEYS[1], 2000)
			return 1
		else
			return 0
		end
	`
	// ARGV: cutoff, limit, score, member
	res, err := redis.NewScript(lua).Run(ctx, rdb, []string{key},
		strconv.FormatInt(oneSecondAgoMs, 10),
		strconv.Itoa(limit),
		strconv.FormatInt(nowMs, 10),
		strconv.FormatInt(nowNs, 10),
	).Int()
	if err != nil {
		// treat redis errors as deny (you could choose to allow on error; here we deny)
		return false
	}
	return res == 1
}

// ----------------------------
// Public unified API
// ----------------------------

// RateLimit is the single public function required by the challenge.
// It returns true if the request is allowed (under the user's limit per second).
//
// It uses per-user configured limit if present; otherwise uses 'limit' parameter.
// If InitRedis has been called, it uses Redis (distributed); otherwise falls back to in-memory.
func RateLimit(userID string, limit int) bool {
	if limit <= 0 {
		return false
	}

	// override with config if exists
	if cfg, ok := GetUserLimit(userID); ok {
		limit = cfg
	}

	// if redis initialized, use it
	if rdb != nil {
		return rateLimitRedis(userID, limit)
	}
	// else use in-memory
	return rateLimitMemory(userID, limit)
}
