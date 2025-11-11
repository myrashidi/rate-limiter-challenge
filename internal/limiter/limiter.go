package limiter

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	// In-memory sliding-window
	userBuckets = sync.Map{} // map[userID][]int64
	userConfig  = sync.Map{} // map[userID]int

	// Redis
	rdb *redis.Client
	ctx = context.Background()
)

// ----------------------------
// Per-user config
// ----------------------------

func SetUserLimit(userID string, limit int) {
	userConfig.Store(userID, limit)
}

func GetUserLimit(userID string) (int, bool) {
	v, ok := userConfig.Load(userID)
	if !ok {
		return 0, false
	}
	return v.(int), true
}

// Load JSON config
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
// In-memory sliding-window limiter
// ----------------------------

func RateLimit(userID string, defaultLimit int) bool {
	limit := defaultLimit
	if cfg, ok := GetUserLimit(userID); ok {
		limit = cfg
	}

	now := time.Now().UnixMilli()
	val, _ := userBuckets.LoadOrStore(userID, &sync.Mutex{})
	mtx := val.(*sync.Mutex)

	mtx.Lock()
	defer mtx.Unlock()

	rawSlice, _ := userBuckets.LoadOrStore(userID+"_slice", &[]int64{})
	tsSlice := rawSlice.(*[]int64)

	// remove old timestamps
	newSlice := []int64{}
	for _, ts := range *tsSlice {
		if now-ts < 1000 {
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

// ----------------------------
// Redis support
// ----------------------------

func InitRedis(addr string, password string, db int) {
	rdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
}

// Distributed sliding-window
func RateLimitRedis(userID string, limit int) bool {
	if rdb == nil || limit <= 0 {
		return false
	}

	now := time.Now().UnixMilli()
	oneSecondAgo := now - 1000
	key := "rate:" + userID

	script := redis.NewScript(`
		redis.call("ZREMRANGEBYSCORE", KEYS[1], 0, ARGV[1])
		local current = redis.call("ZCARD", KEYS[1])
		if tonumber(current) < tonumber(ARGV[2]) then
			redis.call("ZADD", KEYS[1], ARGV[3], ARGV[3])
			redis.call("PEXPIRE", KEYS[1], 2000)
			return 1
		else
			return 0
		end
	`)
	res, err := script.Run(ctx, rdb, []string{key}, oneSecondAgo, limit, now).Int()
	if err != nil {
		return false
	}
	return res == 1
}

// RateLimitRedis with dynamic per-user config
func RateLimitRedisWithConfig(userID string, defaultLimit int) bool {
	limit := defaultLimit
	if cfg, ok := GetUserLimit(userID); ok {
		limit = cfg
	}
	return RateLimitRedis(userID, limit)
}
