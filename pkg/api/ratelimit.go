package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IPRateLimiter provides per-IP rate limiting middleware.
type IPRateLimiter struct {
	visitors map[string]*visitorEntry
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
}

// visitorEntry holds the rate limiter and last seen time for a visitor.
type visitorEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPRateLimiter creates a new IP-based rate limiter.
func NewIPRateLimiter(requestsPerMinute int) *IPRateLimiter {
	rl := &IPRateLimiter{
		visitors: make(map[string]*visitorEntry, 256),
		rate:     rate.Limit(float64(requestsPerMinute) / 60.0),
		burst:    requestsPerMinute,
	}

	// Start cleanup goroutine to remove stale entries.
	go rl.cleanupLoop()

	return rl
}

// getLimiter returns the rate limiter for the given IP, creating one if necessary.
func (l *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.visitors[ip]
	if !exists {
		limiter := rate.NewLimiter(l.rate, l.burst)
		l.visitors[ip] = &visitorEntry{
			limiter:  limiter,
			lastSeen: time.Now(),
		}

		return limiter
	}

	entry.lastSeen = time.Now()

	return entry.limiter
}

// Middleware returns an HTTP middleware that enforces rate limiting per IP.
func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr // chi's RealIP middleware sets this
		limiter := l.getLimiter(ip)

		if !limiter.Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", strconv.Itoa(int(time.Minute.Seconds())))
			w.WriteHeader(http.StatusTooManyRequests)
			//nolint:errcheck // Response writing errors are not recoverable
			w.Write([]byte(`{"error":"rate limit exceeded"}`))

			return
		}

		next.ServeHTTP(w, r)
	})
}

// cleanupLoop periodically removes stale IP entries.
func (l *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		l.cleanup(10 * time.Minute)
	}
}

// cleanup removes entries that haven't been seen for longer than maxAge.
func (l *IPRateLimiter) cleanup(maxAge time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)

	for ip, entry := range l.visitors {
		if entry.lastSeen.Before(cutoff) {
			delete(l.visitors, ip)
		}
	}
}
