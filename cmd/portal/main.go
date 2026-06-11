package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/davidpopovici01/grades/internal/portalserver"
)

func main() {
	cfg := portalserver.Config{
		DataDir:         getEnv("PORTAL_DATA_DIR", "./data"),
		StaticDir:       getEnv("PORTAL_STATIC_DIR", "./static"),
		Addr:            getEnv("PORTAL_ADDR", ":8080"),
		CookieSecure:    getEnvBool("PORTAL_COOKIE_SECURE", false),
		CookieDomain:    getEnv("PORTAL_COOKIE_DOMAIN", ""),
		RateLimitPerMin: getEnvInt("PORTAL_RATE_LIMIT", 300),
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

	log.Printf("Portal server starting on %s", cfg.Addr)
	log.Printf("Data dir: %s", cfg.DataDir)
	log.Printf("Static dir: %s", cfg.StaticDir)

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
