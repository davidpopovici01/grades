#!/bin/bash
set -e

cd "$(dirname "$0")/.."

echo "Building React frontend..."
cd portal-web && npm run build
cd ..

echo "Building Go portal binary..."
GOOS=linux GOARCH=amd64 go build -o dist/portal ./cmd/portal

echo "Build complete: dist/portal"
