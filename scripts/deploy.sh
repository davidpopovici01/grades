#!/bin/bash
set -e

SERVER="${SERVER:-user@singapore-vps}"
REMOTE_DIR="/opt/portal"
DATA_DIR="${REMOTE_DIR}/data"
STATIC_DIR="${REMOTE_DIR}/static"

cd "$(dirname "$0")/.."

echo "Building..."
./scripts/build-portal.sh

echo "Publishing grades locally..."
go run ./cmd/grades web publish ./data || true

echo "Uploading to ${SERVER}..."
rsync -avz --delete dist/portal "${SERVER}:${REMOTE_DIR}/"
rsync -avz --delete data/ "${SERVER}:${DATA_DIR}/"
rsync -avz --delete portal-web/dist/ "${SERVER}:${STATIC_DIR}/"

echo "Restarting portal service..."
ssh "${SERVER}" "sudo systemctl restart portal"

echo "Done!"
