package ratelimit

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type RateLimiter struct {
	connections  map[string]int         // IP -> connection count
	authAttempts map[string][]time.Time // IP -> timestamps of auth attempts
	mu           sync.RWMutex
	maxConns     int
	maxAuth      int
}

func New() *RateLimiter {
	maxConns := 10
	if v := os.Getenv("MAX_CONNECTIONS_PER_IP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxConns = n
		}
	}

	maxAuth := 5
	if v := os.Getenv("AUTH_ATTEMPTS_PER_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxAuth = n
		}
	}

	rl := &RateLimiter{
		connections:  make(map[string]int),
		authAttempts: make(map[string][]time.Time),
		maxConns:     maxConns,
		maxAuth:      maxAuth,
	}

	// Cleanup old auth attempts every minute
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.cleanup()
		}
	}()

	return rl
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-time.Minute)
	for ip, attempts := range rl.authAttempts {
		var valid []time.Time
		for _, t := range attempts {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.authAttempts, ip)
		} else {
			rl.authAttempts[ip] = valid
		}
	}
}

func (rl *RateLimiter) CanConnect(ip string) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.connections[ip] < rl.maxConns
}

func (rl *RateLimiter) AddConnection(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.connections[ip]++
}

func (rl *RateLimiter) RemoveConnection(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.connections[ip]--
	if rl.connections[ip] <= 0 {
		delete(rl.connections, ip)
	}
}

func (rl *RateLimiter) CanAuth(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-time.Minute)
	var recent []time.Time
	for _, t := range rl.authAttempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	rl.authAttempts[ip] = recent

	if len(recent) >= rl.maxAuth {
		return false
	}

	rl.authAttempts[ip] = append(rl.authAttempts[ip], time.Now())
	return true
}

func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}
