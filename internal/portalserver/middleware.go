package portalserver

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// securityHeaders adds recommended security headers to all responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware allows same-origin and localhost development requests.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request with method, path, status, and duration.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)
		fmt.Printf("[%s] %s %s %d %s\n", start.Format(time.RFC3339), r.Method, r.URL.Path, wrapped.statusCode, duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// rateLimiter implements a simple per-IP token bucket rate limiter.
type rateLimiter struct {
	mu       sync.RWMutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
}

type visitor struct {
	tokens    int
	lastSeen  time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &visitor{tokens: rl.limit - 1, lastSeen: now}
		return true
	}

	// Refill tokens based on time elapsed.
	elapsed := now.Sub(v.lastSeen)
	tokensToAdd := int(elapsed * time.Duration(rl.limit) / rl.window)
	if tokensToAdd > 0 {
		v.tokens = min(v.tokens+tokensToAdd, rl.limit)
		v.lastSeen = now
	}

	if v.tokens > 0 {
		v.tokens--
		return true
	}
	return false
}

// rateLimitMiddleware enforces per-IP rate limiting.
func rateLimitMiddleware(rl *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if !rl.allow(ip) {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
