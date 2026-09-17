#!/bin/bash
set -e

cd "$(dirname "$0")/.."

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

echo "Building React frontend..."
cd portal-web && npm run build
cd ..

echo "Building Go portal binary (version ${VERSION})..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-s -w -X github.com/davidpopovici01/grades/internal/portalserver.Version=${VERSION}" \
  -o dist/portal ./cmd/portal

echo "Build complete: dist/portal"
