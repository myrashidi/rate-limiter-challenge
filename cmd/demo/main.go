package main

import (
	"fmt"
	"net/http"

	"github.com/myrashidi/rate-limiter-challenge/internal/limiter"
)

func main() {
	// Load per-user limits from a file (optional)
	_ = limiter.LoadUserConfigFromJSON("config/users.json")

	// Or set some dynamically
	limiter.SetUserLimit("alice", 5)
	limiter.SetUserLimit("bob", 10)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		if user == "" {
			user = "guest"
		}

		if !limiter.RateLimit(user, 3) { // default 3 if user not configured
			http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
			return
		}

		fmt.Fprintf(w, "Hello %s! Your request is allowed.\n", user)
	})

	fmt.Println("Server running on :8080")
	http.ListenAndServe(":8080", nil)
}
