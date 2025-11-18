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
	userSlices  = sync.Map{} // map[string]*[]int64 (for sliding)
	userConfig  = sync.Map{} // map[string]int

	// leaky-bucket in-memory: per-user state
	leakyBuckets = sync.Map{} // map[userID]*leakyState

	// redis
	rdb *redis.Client
	ctx = context.Background()

	// global mode: "sliding" (default) or "leaky"
	globalModeMu sync.RWMutex
	globalMode   = "sliding"
)

// leakyState holds in-memory leaky bucket state
type leakyState struct {
	mtx        sync.Mutex
	tokens     float64 // current tokens in bucket
	lastMillis int64   // last updated timestamp in ms
	capacity   float64 // bucket capacity (max tokens)
	ratePerMs  float64 // refill rate in tokens per millisecond
}

// ----------------------------
// Mode control
// ----------------------------

// SetMode sets the global algorithm mode: "sliding" or "leaky"
func SetMode(mode string) {
	globalModeMu.Lock()
	defer globalModeMu.Unlock()
	if mode == "sliding" || mode == "leaky" {
		globalMode = mode
	}
}

// GetMode returns current global mode
func GetMode() string {
	globalModeMu.RLock()
	defer globalModeMu.RUnlock()
	return globalMode
}

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
	// support both simple map[string]int and extended map[string]struct (not required now)
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

// ---------- Sliding-window (in-memory) ----------
func rateLimitMemorySliding(userID string, limit int) bool {
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
	// reuse slice backing if possible
	newSlice := (*tsSlice)[:0]
	for _, ts := range *tsSlice {
		if ts > cutoff {
			newSlice = append(newSlice, ts)
		}
	}
	if len(newSlice) >= limit {
		*tsSlice = newSlice
		return false
	}
	newSlice = append(newSlice, now)
	*tsSlice = newSlice
	return true
}

// ---------- Sliding-window (Redis) ----------
func rateLimitRedisSliding(userID string, limit int) bool {
	if rdb == nil || limit <= 0 {
		return false
	}
	t := time.Now()
	nowMs := t.UnixMilli()
	nowNs := t.UnixNano()
	oneSecondAgoMs := nowMs - 1000
	key := "rate:" + userID

	const lua = `
		-- remove timestamps older than cutoff
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
	res, err := redis.NewScript(lua).Run(ctx, rdb, []string{key},
		strconv.FormatInt(oneSecondAgoMs, 10),
		strconv.Itoa(limit),
		strconv.FormatInt(nowMs, 10),
		strconv.FormatInt(nowNs, 10),
	).Int()
	if err != nil {
		return false
	}
	return res == 1
}

// ---------- Leaky-bucket (in-memory) ----------
func rateLimitMemoryLeaky(userID string, limit int) bool {
	// config: capacity = limit (requests), leak rate = limit tokens / 1000ms
	capacity := float64(limit)
	ratePerMs := float64(limit) / 1000.0 // tokens per millisecond

	val, _ := leakyBuckets.LoadOrStore(userID, &leakyState{
		tokens:     capacity,
		lastMillis: time.Now().UnixMilli(),
		capacity:   capacity,
		ratePerMs:  ratePerMs,
	})
	st := val.(*leakyState)

	now := time.Now().UnixMilli()
	st.mtx.Lock()
	defer st.mtx.Unlock()

	// refill tokens
	elapsed := float64(now - st.lastMillis)
	if elapsed < 0 {
		elapsed = 0
	}
	refill := elapsed * st.ratePerMs
	st.tokens += refill
	if st.tokens > st.capacity {
		st.tokens = st.capacity
	}
	st.lastMillis = now

	// consume one token
	if st.tokens >= 1.0 {
		st.tokens -= 1.0
		return true
	}
	// not enough tokens
	return false
}

// ---------- Leaky-bucket (Redis) ----------
func rateLimitRedisLeaky(userID string, limit int) bool {
	if rdb == nil || limit <= 0 {
		return false
	}
	// capacity = limit tokens; rate per ms = limit/1000
	t := time.Now()
	nowMs := t.UnixMilli()
	key := "bucket:" + userID

	// Lua script:
	// KEYS[1] = key
	// ARGV[1] = nowMs
	// ARGV[2] = capacity (number)
	// ARGV[3] = ratePerMs (tokens per ms, as number)
	// Behavior:
	// - read tokens,last
	// - compute leaked = (now-last)*ratePerMs
	// - tokens = min(capacity, tokens + leaked)
	// - if tokens >= 1: tokens -= 1; store tokens,last=now; PEXPIRE; return 1
	// - else store tokens,last=now; return 0
	const lua = `
		local key = KEYS[1]
		local now = tonumber(ARGV[1])
		local capacity = tonumber(ARGV[2])
		local rate = tonumber(ARGV[3])

		local data = redis.call("HMGET", key, "tokens", "last")
		local tokens = tonumber(data[1])
		local last = tonumber(data[2])
		if tokens == nil then tokens = capacity end
		if last == nil then last = now end

		local elapsed = now - last
		if elapsed < 0 then elapsed = 0 end
		local leaked = elapsed * rate
		tokens = tokens + leaked
		if tokens > capacity then tokens = capacity end

		if tokens >= 1 then
			tokens = tokens - 1
			redis.call("HMSET", key, "tokens", tostring(tokens), "last", tostring(now))
			redis.call("PEXPIRE", key, 2000)
			return 1
		else
			redis.call("HMSET", key, "tokens", tostring(tokens), "last", tostring(now))
			redis.call("PEXPIRE", key, 2000)
			return 0
		end
	`

	capacityStr := strconv.FormatFloat(float64(limit), 'f', -1, 64)
	rateStr := strconv.FormatFloat(float64(limit)/1000.0, 'f', -8, 64)

	res, err := redis.NewScript(lua).Run(ctx, rdb, []string{key},
		strconv.FormatInt(nowMs, 10),
		capacityStr,
		rateStr,
	).Int()
	if err != nil {
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
// If InitRedis has been called, Redis-backed implementation is used (distributed).
// The algorithm used (sliding or leaky) is determined by global mode (SetMode/GetMode).
func RateLimit(userID string, limit int) bool {
	if limit <= 0 {
		return false
	}

	// override with config if exists
	if cfg, ok := GetUserLimit(userID); ok && cfg > 0 {
		limit = cfg
	}

	mode := GetMode()
	// prefer Redis if initialized
	if rdb != nil {
		if mode == "leaky" {
			return rateLimitRedisLeaky(userID, limit)
		}
		return rateLimitRedisSliding(userID, limit)
	}

	// in-memory fallback
	if mode == "leaky" {
		return rateLimitMemoryLeaky(userID, limit)
	}
	return rateLimitMemorySliding(userID, limit)
}
