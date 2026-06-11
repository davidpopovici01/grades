#!/bin/bash
set -e

cd "$(dirname "$0")/.."

# Use a local data directory for testing
DATA_DIR="${1:-./data}"
STATIC_DIR="./portal-web/dist"

# Build frontend if needed
if [ ! -d "$STATIC_DIR" ]; then
    echo "Building frontend..."
    cd portal-web && npm run build
    cd ..
fi

# Build portal binary for local OS
echo "Building portal binary..."
go build -o dist/portal-local ./cmd/portal

# Generate a test JWT secret if not present
SECRET_FILE=".jwt-secret-local"
if [ ! -f "$SECRET_FILE" ]; then
    openssl rand -base64 32 > "$SECRET_FILE"
    echo "Generated test JWT secret: $SECRET_FILE"
fi

# Publish grades if data dir doesn't exist or is empty
if [ ! -f "$DATA_DIR/accounts.json" ]; then
    echo "Publishing grades to $DATA_DIR..."
    go run ./cmd/grades web publish "$DATA_DIR"
fi

echo ""
echo "Starting local portal server..."
echo "  Data dir:   $DATA_DIR"
echo "  Static dir: $STATIC_DIR"
echo "  URL:        http://localhost:8080"
echo ""

PORTAL_DATA_DIR="$DATA_DIR" \
PORTAL_STATIC_DIR="$STATIC_DIR" \
PORTAL_JWT_SECRET_FILE="$SECRET_FILE" \
PORTAL_ADDR="localhost:8080" \
PORTAL_COOKIE_SECURE="false" \
    ./dist/portal-local
