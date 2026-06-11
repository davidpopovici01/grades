package portalserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

// Config holds the portal server configuration.
type Config struct {
	DataDir          string
	StaticDir        string
	JWTSecret        []byte
	Addr             string
	CookieSecure     bool
	CookieDomain     string
	RateLimitPerMin  int
}

// Server is the stateless student portal HTTP server.
type Server struct {
	config          Config
	jwt             *JWTHelper
	mu              sync.RWMutex
	accounts        map[string]portalauth.Account // keyed by lowercase username
	students        map[int]portalauth.Account    // keyed by studentId
	accountsLoaded  time.Time                     // when accounts.json was last loaded
	passwordChanges map[int]portalauth.Account    // local server-side password changes
}

// NewServer creates a new portal server, loading account data from disk.
func NewServer(cfg Config) (*Server, error) {
	if len(cfg.JWTSecret) == 0 {
		return nil, fmt.Errorf("JWT secret is required")
	}

	s := &Server{
		config:          cfg,
		jwt:             NewJWTHelper(cfg.JWTSecret),
		accounts:        make(map[string]portalauth.Account),
		students:        make(map[int]portalauth.Account),
		passwordChanges: make(map[int]portalauth.Account),
	}

	if err := s.reloadAccounts(); err != nil {
		return nil, fmt.Errorf("failed to load accounts: %w", err)
	}

	return s, nil
}

// maybeReloadAccounts checks if accounts.json has changed and reloads it.
func (s *Server) maybeReloadAccounts() error {
	path := filepath.Join(s.config.DataDir, "accounts.json")
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.ModTime().After(s.accountsLoaded) {
		return s.reloadAccounts()
	}
	return nil
}

// reloadAccounts reads accounts.json from the data directory and merges with local password changes.
func (s *Server) reloadAccounts() error {
	path := filepath.Join(s.config.DataDir, "accounts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read accounts.json: %w", err)
	}

	var list portalauth.AccountList
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("cannot parse accounts.json: %w", err)
	}

	// Load local password changes.
	s.loadPasswordChanges()

	newAccounts := make(map[string]portalauth.Account, len(list.Accounts))
	newStudents := make(map[int]portalauth.Account, len(list.Accounts))
	for _, acc := range list.Accounts {
		// If server has a newer password change, use it.
		if local, ok := s.passwordChanges[acc.StudentID]; ok {
			if local.PasswordChangedAt > acc.PasswordChangedAt {
				acc = local
			}
		}
		newAccounts[strings.ToLower(acc.Username)] = acc
		newStudents[acc.StudentID] = acc
	}

	s.accounts = newAccounts
	s.students = newStudents
	s.accountsLoaded = time.Now()
	return nil
}

// loadPasswordChanges reads password-changes.json from the data directory.
func (s *Server) loadPasswordChanges() {
	path := filepath.Join(s.config.DataDir, "password-changes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return // file may not exist yet
	}
	var list portalauth.AccountList
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	for _, acc := range list.Accounts {
		s.passwordChanges[acc.StudentID] = acc
	}
}

// savePasswordChanges writes password-changes.json to the data directory.
func (s *Server) savePasswordChanges() error {
	var accounts []portalauth.Account
	for _, acc := range s.passwordChanges {
		accounts = append(accounts, acc)
	}
	list := portalauth.AccountList{
		Version:     1,
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
		Accounts:    accounts,
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(s.config.DataDir, "password-changes.json")
	return os.WriteFile(path, data, 0o644)
}

// Handler returns the HTTP handler with all routes and middleware applied.
func (s *Server) Handler() http.Handler {
	limit := s.config.RateLimitPerMin
	if limit <= 0 {
		limit = 300
	}
	rl := newRateLimiter(limit, time.Minute)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/change-password", s.handleChangePassword)
	mux.HandleFunc("/api/grades", s.handleGrades)
	mux.HandleFunc("/api/index", s.handleIndex)

	// Static files and SPA fallback.
	fs := http.FileServer(http.Dir(s.config.StaticDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If the path is an API route, it was already handled above.
		// Otherwise try to serve a static file.
		path := filepath.Join(s.config.StaticDir, r.URL.Path)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			// Fall back to index.html for client-side routing.
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
