#!/bin/bash
set -e

SERVER="${SERVER:-user@singapore-vps}"
REMOTE_DIR="/opt/portal"
STATIC_DIR="${REMOTE_DIR}/static"

cd "$(dirname "$0")/.."

echo "Building..."
./scripts/build-portal.sh

echo "Uploading to ${SERVER}..."
rsync -avz --delete dist/portal "${SERVER}:${REMOTE_DIR}/"
rsync -avz --delete portal-web/dist/ "${SERVER}:${STATIC_DIR}/"

echo "Restarting portal service..."
ssh "${SERVER}" "sudo systemctl restart portal"

echo "Done!"
