#!/bin/bash
# SFTP-only deployment (no SSH required)
# Uploads data and static files. Server must already be running.

set -e

SERVER="${SERVER:-user@singapore-vps}"
REMOTE_DIR="/opt/portal"
DATA_DIR="${REMOTE_DIR}/data"
STATIC_DIR="${REMOTE_DIR}/static"

cd "$(dirname "$0")/.."

echo "Building frontend..."
cd portal-web && npm run build
cd ..

echo "Publishing grades locally..."
go run . publish ./data

echo "Uploading to ${SERVER} via SFTP..."

# Create batch file for sftp
mkdir -p .tmp
{
  echo "cd ${REMOTE_DIR}"
  echo "put -r data"
  echo "put -r portal-web/dist"
  echo "bye"
} > .tmp/sftp-batch.txt

sftp -b .tmp/sftp-batch.txt "${SERVER}"

echo ""
echo "Upload complete!"
echo "Note: The running server will pick up new data on next request."
echo "If you changed the Go binary, you need SSH to restart the service."
