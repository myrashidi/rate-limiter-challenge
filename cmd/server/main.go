package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/myrashidi/rate-limiter-challenge/internal/limiter"
)

func main() {
	// Set mode from env if present: "sliding" or "leaky"
	mode := getenv("RATE_LIMIT_MODE", "sliding")
	limiter.SetMode(mode)
	log.Printf("Rate limiter mode: %s", limiter.GetMode())

	// Load config first (optional)
	if err := limiter.LoadUserConfigFromJSON("config/users.json"); err != nil {
		log.Printf("No config loaded (this is fine for demo): %v", err)
	}

	// Init redis (optional). If you want pure in-memory mode, don't call InitRedis.
	addr := getenv("REDIS_ADDR", "localhost:6379")
	pass := getenv("REDIS_PASSWORD", "")
	db := getenvInt("REDIS_DB", 0)
	limiter.InitRedis(addr, pass, db)

	http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		if user == "" {
			http.Error(w, "missing user parameter", http.StatusBadRequest)
			return
		}

		// Default limit if user not configured
		defaultLimit := 5
		if !limiter.RateLimit(user, defaultLimit) {
			http.Error(w, fmt.Sprintf("Rate limit exceeded for user %s", user), http.StatusTooManyRequests)
			return
		}

		fmt.Fprintf(w, "Request allowed for user %s\n", user)
	})

	log.Println("Rate limiter demo server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func getenv(key string, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
