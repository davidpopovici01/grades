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

// Version is the portal build version, stamped via ldflags at release time.
var Version = "dev"

// Config holds the portal server configuration.
type Config struct {
	StaticDir       string
	DBPath          string
	JWTSecret       []byte
	TeacherToken    string
	Addr            string
	CookieSecure    bool
	CookieDomain    string
	RateLimitPerMin int
	MaterialsDir    string
	SubmissionsDir  string
	JPlagJar        string
	DemoPassword    string
}

// Server is the student portal HTTP server, backed by a SQLite store.
type Server struct {
	config    Config
	jwt       *JWTHelper
	store     *Store
	grader    *Grader
	cooldowns *cooldownTracker
	// demoEnabled reports whether the read-only demo account was seeded.
	demoEnabled bool
	// plagViewerDir holds the report viewer extracted from the JPlag jar;
	// empty when the jar or its bundled viewer is missing.
	plagViewerDir string
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
	if cfg.MaterialsDir == "" {
		cfg.MaterialsDir = "./materials"
	}
	if cfg.SubmissionsDir == "" {
		cfg.SubmissionsDir = "./submissions"
	}
	if cfg.JPlagJar == "" {
		cfg.JPlagJar = "/opt/portal/lib/jplag.jar"
	}
	store, err := NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open portal database: %w", err)
	}

	server := &Server{
		config:      cfg,
		jwt:         NewJWTHelper(cfg.JWTSecret),
		store:       store,
		cooldowns:   newCooldownTracker(),
		demoEnabled: cfg.DemoPassword != "",
	}
	if server.demoEnabled {
		if err := store.SeedDemo(cfg.DemoPassword); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("failed to seed demo account: %w", err)
		}
		if err := server.seedDemoMaterials(); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("failed to seed demo materials: %w", err)
		}
	} else if err := store.ClearDemo(); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("failed to clear demo account: %w", err)
	}
	server.plagViewerDir = extractPlagViewer(cfg.JPlagJar)
	server.grader = newGrader(store, cfg.SubmissionsDir, cfg.JPlagJar)
	go server.grader.run()
	return server, nil
}

// Close releases server resources.
func (s *Server) Close() error {
	s.grader.close()
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

	// Unauthenticated liveness probe for deploy verification and uptime checks.
	mux.HandleFunc("/api/health", s.handleHealth)

	// Student routes
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/change-password", s.handleChangePassword)
	mux.HandleFunc("/api/grades", s.handleGrades)
	mux.HandleFunc("/api/index", s.handleIndex)

	// Admin routes
	mux.HandleFunc("/api/admin/session", s.adminAuth(s.handleAdminSession))
	mux.HandleFunc("/api/admin/publish", s.adminAuth(s.handleAdminPublish))
	mux.HandleFunc("/api/admin/activity", s.adminAuth(s.handleAdminActivity))
	mux.HandleFunc("/api/admin/courses", s.adminAuth(s.handleAdminListCourses))
	mux.HandleFunc("/api/admin/courses/", s.adminAuth(s.handleAdminCourseRoutes))
	mux.HandleFunc("/api/admin/students/", s.adminAuth(s.handleAdminResetPassword))

	// Materials routes
	mux.HandleFunc("/api/materials", s.handleMaterials)
	mux.HandleFunc("/api/materials/download/", s.handleMaterialDownload)
	mux.HandleFunc("/api/admin/materials", s.adminAuth(s.handleAdminMaterials))
	mux.HandleFunc("/api/admin/materials/upload", s.adminAuth(s.handleAdminMaterialUpload))
	mux.HandleFunc("/api/admin/materials/delete", s.adminAuth(s.handleAdminMaterialDelete))
	mux.HandleFunc("/api/admin/materials/rename", s.adminAuth(s.handleAdminMaterialRename))
	mux.HandleFunc("/api/admin/materials/move", s.adminAuth(s.handleAdminMaterialMove))
	mux.HandleFunc("/api/admin/materials/categories", s.adminAuth(s.handleAdminMaterialCategories))
	mux.HandleFunc("/api/admin/materials/categories/rename", s.adminAuth(s.handleAdminCategoryRename))
	mux.HandleFunc("/api/admin/materials/categories/reorder", s.adminAuth(s.handleAdminCategoryReorder))

	// Submissions routes
	mux.HandleFunc("/api/submissions", s.handleStudentSubmissions)
	mux.HandleFunc("/api/submissions/{id}", s.handleStudentSubmissionDetail)
	mux.HandleFunc("/api/submissions/{id}/files", s.handleStudentSubmissionUpload)
	mux.HandleFunc("/api/submissions/{id}/slot/{name}", s.handleStudentSubmissionSlot)
	mux.HandleFunc("/api/submissions/{id}/submit", s.handleStudentSubmissionSubmit)
	mux.HandleFunc("/api/submissions/{id}/test", s.handleStudentSubmissionTest)
	mux.HandleFunc("/api/submission-files/{id}/{name}", s.handleStudentSubmissionFile)
	mux.HandleFunc("/api/admin/submissions/assignments", s.adminAuth(s.handleAdminSubAssignments))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}", s.adminAuth(s.handleAdminSubAssignment))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/tests", s.adminAuth(s.handleAdminSubTestUpload))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/tests/{testId}", s.adminAuth(s.handleAdminSubTestDelete))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/sample", s.adminAuth(s.auxFilesHandler("sample")))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/sample/files/{name}", s.adminAuth(s.auxFileHandler("sample")))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/sample/run", s.adminAuth(s.handleAdminSubSampleRun))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/basecode", s.adminAuth(s.auxFilesHandler("basecode")))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/basecode/files/{name}", s.adminAuth(s.auxFileHandler("basecode")))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/submissions", s.adminAuth(s.handleAdminSubAssignmentSubmissions))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/run", s.adminAuth(s.handleAdminSubRun))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/plagiarism", s.adminAuth(s.handleAdminSubPlagiarism))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/plagiarism/report", s.adminAuth(s.handleAdminSubPlagReport))
	mux.HandleFunc("/api/admin/submissions/assignments/{id}/download", s.adminAuth(s.handleAdminSubAssignmentDownload))
	mux.HandleFunc("/api/admin/submissions/submissions/{id}", s.adminAuth(s.handleAdminSubSubmissionDetail))
	mux.HandleFunc("/api/admin/submissions/submissions/{id}/files/{name}", s.adminAuth(s.handleAdminSubSubmissionFile))
	mux.HandleFunc("/api/admin/submissions/queue", s.adminAuth(s.handleAdminSubQueue))

	// Static files and SPA fallback. Unknown /api paths get a JSON 404 so the
	// frontend never parses index.html as an API response.
	fs := http.FileServer(http.Dir(s.config.StaticDir))
	viewerFS := http.FileServer(http.Dir(s.plagViewerDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		// The JPlag report viewer owns its client-side routes at the origin
		// root (its router is root-relative). The report data it loads stays
		// behind admin auth.
		if plagViewerPage(r.URL.Path) {
			if s.plagViewerDir == "" {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report viewer not available (JPlag jar missing or too old)"})
				return
			}
			http.ServeFile(w, r, filepath.Join(s.plagViewerDir, "index.html"))
			return
		}
		path := filepath.Join(s.config.StaticDir, r.URL.Path)
		info, err := os.Stat(path)
		if (err != nil || info.IsDir()) && s.plagViewerDir != "" {
			// Viewer assets (/assets/… of the viewer, /favicon.ico) share the
			// origin root with the portal's own hashed assets.
			if vinfo, verr := os.Stat(filepath.Join(s.plagViewerDir, r.URL.Path)); verr == nil && !vinfo.IsDir() {
				viewerFS.ServeHTTP(w, r)
				return
			}
		}
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
	handler = s.loggingMiddleware(handler)

	return handler
}

// handleHealth reports liveness and the build version; no auth required.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
}

// isDemo reports whether the claims belong to the shared read-only demo account.
func (s *Server) isDemo(claims *PortalClaims) bool {
	return s.demoEnabled && claims.Username == demoUsername
}

// adminCookieName carries the teacher token as an HttpOnly cookie for
// browser-embedded admin tools (the report viewer) that cannot send an
// Authorization header.
const adminCookieName = "portal_admin"

// adminAuth protects admin routes with a bearer token or the admin session
// cookie (set by handleAdminSession for the embedded report viewer).
func (s *Server) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := s.config.TeacherToken
		if token == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "admin API not configured"})
			return
		}
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(auth, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == token {
			next(w, r)
			return
		}
		if c, err := r.Cookie(adminCookieName); err == nil && c.Value == token {
			next(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
}

// handleAdminSession sets the admin session cookie. It requires bearer auth,
// so only a logged-in admin can obtain it. SameSite=Strict keeps the cookie
// from being sent cross-site.
func (s *Server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    s.config.TeacherToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
	claims, err := s.jwt.Verify(cookie.Value)
	if err != nil {
		return nil, err
	}
	if err := s.store.TouchLastSeen(claims.StudentID); err != nil {
		fmt.Printf("last-seen update failed for %s: %v\n", claims.Username, err)
	}
	return claims, nil
}

// setTokenCookie sets the JWT cookie. When CookieDomain is configured (e.g.
// "example.com"), the cookie is shared with sibling subdomains so a login on
// one subdomain persists on the others.
func (s *Server) setTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Domain:   s.config.CookieDomain,
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(tokenDuration.Seconds()),
	})
}

// clearTokenCookie removes the JWT cookie, clearing both the domain-wide and
// any legacy host-only variant so no stale cookie survives.
func (s *Server) clearTokenCookie(w http.ResponseWriter) {
	base := http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
	if s.config.CookieDomain != "" {
		domainCookie := base
		domainCookie.Domain = s.config.CookieDomain
		http.SetCookie(w, &domainCookie)
	}
	http.SetCookie(w, &base)
}
