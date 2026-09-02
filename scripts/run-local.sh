#!/bin/bash
# Run the student portal server locally for testing.
# Serves the React frontend from portal-web/dist with a throwaway SQLite DB.
set -e

cd "$(dirname "$0")/.."

STATIC_DIR="./portal-web/dist"
LOCAL_DIR="./dist/portal-local"

# Build frontend if needed
if [ ! -d "$STATIC_DIR" ]; then
    echo "Building frontend..."
    (cd portal-web && npm ci && npm run build)
fi

mkdir -p "$LOCAL_DIR"

# Generate a test JWT secret and teacher token if not present
if [ ! -f "$LOCAL_DIR/jwt-secret" ]; then
    openssl rand -base64 32 > "$LOCAL_DIR/jwt-secret"
fi
if [ ! -f "$LOCAL_DIR/teacher-token" ]; then
    openssl rand -base64 32 > "$LOCAL_DIR/teacher-token"
fi
TOKEN="$(cat "$LOCAL_DIR/teacher-token")"

echo ""
echo "Starting local portal server..."
echo "  URL:           http://localhost:8080"
echo "  Admin UI:      http://localhost:8080/admin"
echo "  Teacher token: $TOKEN"
echo "  Database:      $LOCAL_DIR/grades-portal.db"
echo ""
echo "To push grades to this server, set in ~/.grades/config.yaml:"
echo "  portal.url: http://localhost:8080"
echo "  portal.teacher_token: $TOKEN"
echo "then run: grades publish"
echo ""
echo "For a quick preview without this server, use: grades web serve"
echo ""

PORTAL_DB_PATH="$LOCAL_DIR/grades-portal.db" \
PORTAL_STATIC_DIR="$STATIC_DIR" \
PORTAL_JWT_SECRET_FILE="$LOCAL_DIR/jwt-secret" \
PORTAL_TEACHER_TOKEN_FILE="$LOCAL_DIR/teacher-token" \
PORTAL_ADDR="localhost:8080" \
PORTAL_COOKIE_SECURE="false" \
    go run ./cmd/portal
