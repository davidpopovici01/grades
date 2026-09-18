package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/davidpopovici01/grades/internal/portalserver"
)

// main loads the portal configuration and starts the HTTP server.
func main() {
	cfg := portalserver.Config{
		StaticDir:       getEnv("PORTAL_STATIC_DIR", "./static"),
		DBPath:          getEnv("PORTAL_DB_PATH", filepath.Join(os.TempDir(), "grades-portal.db")),
		Addr:            getEnv("PORTAL_ADDR", ":8080"),
		CookieSecure:    getEnvBool("PORTAL_COOKIE_SECURE", false),
		CookieDomain:    getEnv("PORTAL_COOKIE_DOMAIN", ""),
		RateLimitPerMin: getEnvInt("PORTAL_RATE_LIMIT", 300),
		MaterialsDir:    getEnv("PORTAL_MATERIALS_DIR", "./materials"),
		SubmissionsDir:  getEnv("PORTAL_SUBMISSIONS_DIR", "./submissions"),
		JPlagJar:        getEnv("PORTAL_JPLAG_JAR", "/opt/portal/lib/jplag.jar"),
		TeacherToken:    getTeacherToken(),
		DemoPassword:    getDemoPassword(),
	}

	secret := getJWTSecret()
	if secret == nil {
		log.Fatal("PORTAL_JWT_SECRET or PORTAL_JWT_SECRET_FILE must be set")
	}
	cfg.JWTSecret = secret

	server, err := portalserver.NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	log.Printf("Portal server starting on %s (version %s)", cfg.Addr, portalserver.Version)
	log.Printf("Static dir: %s", cfg.StaticDir)
	if cfg.TeacherToken == "" {
		log.Println("Warning: PORTAL_TEACHER_TOKEN not set; admin endpoints are disabled")
	}

	if err := http.ListenAndServe(cfg.Addr, server.Handler()); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	v := strings.ToLower(os.Getenv(key))
	switch v {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	case "":
		return defaultValue
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultValue
}

func getJWTSecret() []byte {
	if v := os.Getenv("PORTAL_JWT_SECRET"); v != "" {
		return []byte(v)
	}
	if path := os.Getenv("PORTAL_JWT_SECRET_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Warning: cannot read JWT secret file: %v", err)
			return nil
		}
		return []byte(strings.TrimSpace(string(data)))
	}
	return nil
}

func getTeacherToken() string {
	if v := os.Getenv("PORTAL_TEACHER_TOKEN"); v != "" {
		return v
	}
	if path := os.Getenv("PORTAL_TEACHER_TOKEN_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Warning: cannot read teacher token file: %v", err)
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	return ""
}

func getDemoPassword() string {
	if v := os.Getenv("PORTAL_DEMO_PASSWORD"); v != "" {
		return v
	}
	if path := os.Getenv("PORTAL_DEMO_PASSWORD_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Warning: cannot read demo password file: %v", err)
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	return ""
}
