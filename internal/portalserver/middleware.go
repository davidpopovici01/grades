package portalserver

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
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
// The production SPA is served same-origin and needs no CORS headers; only
// localhost/127.0.0.1 origins (any port) are reflected, since credentialed
// reflection of arbitrary origins would let any website make authenticated
// cross-origin calls.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && isDevOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isDevOrigin reports whether the origin is a localhost development origin
// (localhost or 127.0.0.1, any port and scheme).
func isDevOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1"
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

// visitorIdleTTL is how long a visitor entry may go unused before eviction.
const visitorIdleTTL = 10 * time.Minute

// rateLimiter implements a simple per-IP token bucket rate limiter.
// Idle visitor entries are evicted periodically so the map stays bounded on
// long-running processes.
type rateLimiter struct {
	mu        sync.Mutex
	visitors  map[string]*visitor
	limit     int
	window    time.Duration
	lastSweep time.Time
}

type visitor struct {
	tokens   int
	lastSeen time.Time
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
	if now.Sub(rl.lastSweep) >= rl.window {
		rl.sweep(now)
	}

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

// sweep evicts visitors that have been idle longer than visitorIdleTTL.
// Callers must hold rl.mu.
func (rl *rateLimiter) sweep(now time.Time) {
	rl.lastSweep = now
	for ip, v := range rl.visitors {
		if now.Sub(v.lastSeen) > visitorIdleTTL {
			delete(rl.visitors, ip)
		}
	}
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
