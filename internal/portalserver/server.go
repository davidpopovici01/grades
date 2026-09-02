package portalserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config holds the portal server configuration.
type Config struct {
	StaticDir       string
	DBPath          string
	JWTSecret       []byte
	TeacherToken    string
	Addr            string
	CookieSecure    bool
	RateLimitPerMin int
}

// Server is the student portal HTTP server, backed by a SQLite store.
type Server struct {
	config Config
	jwt    *JWTHelper
	store  *Store
}

// NewServer creates a new portal server, opening (and migrating) the SQLite
// store at cfg.DBPath, defaulting to a temp-dir database when empty.
func NewServer(cfg Config) (*Server, error) {
	if len(cfg.JWTSecret) == 0 {
		return nil, fmt.Errorf("JWT secret is required")
	}

	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = filepath.Join(os.TempDir(), "grades-portal.db")
	}
	store, err := NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open portal database: %w", err)
	}

	return &Server{
		config: cfg,
		jwt:    NewJWTHelper(cfg.JWTSecret),
		store:  store,
	}, nil
}

// Close releases server resources.
func (s *Server) Close() error {
	return s.store.Close()
}

// Handler returns the HTTP handler with all routes and middleware applied.
func (s *Server) Handler() http.Handler {
	limit := s.config.RateLimitPerMin
	if limit <= 0 {
		limit = 300
	}
	rl := newRateLimiter(limit, time.Minute)

	mux := http.NewServeMux()

	// Student routes
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/change-password", s.handleChangePassword)
	mux.HandleFunc("/api/grades", s.handleGrades)
	mux.HandleFunc("/api/index", s.handleIndex)

	// Admin routes
	mux.HandleFunc("/api/admin/publish", s.adminAuth(s.handleAdminPublish))
	mux.HandleFunc("/api/admin/courses", s.adminAuth(s.handleAdminListCourses))
	mux.HandleFunc("/api/admin/courses/", s.adminAuth(s.handleAdminCourseRoutes))
	mux.HandleFunc("/api/admin/students/", s.adminAuth(s.handleAdminResetPassword))

	// Static files and SPA fallback. Unknown /api paths get a JSON 404 so the
	// frontend never parses index.html as an API response.
	fs := http.FileServer(http.Dir(s.config.StaticDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		path := filepath.Join(s.config.StaticDir, r.URL.Path)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(s.config.StaticDir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})

	var handler http.Handler = mux
	handler = corsMiddleware(handler)
	handler = securityHeaders(handler)
	handler = rateLimitMiddleware(rl)(handler)
	handler = loggingMiddleware(handler)

	return handler
}

// adminAuth protects admin routes with a bearer token.
func (s *Server) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := s.config.TeacherToken
		if token == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "admin API not configured"})
			return
		}
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) != token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// cookieName is the name of the JWT cookie.
const cookieName = "portal_token"

// tokenDuration is how long a login session lasts.
const tokenDuration = 24 * time.Hour

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// readToken extracts the JWT token from the cookie.
func (s *Server) readToken(r *http.Request) (*PortalClaims, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil, err
	}
	return s.jwt.Verify(cookie.Value)
}

// setTokenCookie sets the JWT cookie.
func (s *Server) setTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(tokenDuration.Seconds()),
	})
}

// clearTokenCookie removes the JWT cookie.
func (s *Server) clearTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
