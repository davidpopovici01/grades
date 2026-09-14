#!/bin/bash
set -e

SERVER="${SERVER:-root@185.223.207.226}"
REMOTE_DIR="/opt/portal"
STATIC_DIR="${REMOTE_DIR}/static"

cd "$(dirname "$0")/.."

echo "Building..."
./scripts/build-portal.sh

echo "Uploading to ${SERVER}..."
rsync -avz --delete dist/portal "${SERVER}:${REMOTE_DIR}/"
rsync -avz --delete portal-web/dist/ "${SERVER}:${STATIC_DIR}/"

echo "Updating systemd unit and data directories..."
rsync -avz scripts/portal.service "${SERVER}:/tmp/portal.service"
ssh "${SERVER}" "sudo mv /tmp/portal.service /etc/systemd/system/portal.service && sudo systemctl daemon-reload"
ssh "${SERVER}" "sudo mkdir -p ${REMOTE_DIR}/materials ${REMOTE_DIR}/submissions ${REMOTE_DIR}/lib && sudo chown portal:portal ${REMOTE_DIR}/materials ${REMOTE_DIR}/submissions ${REMOTE_DIR}/lib"

echo "Restarting portal service..."
ssh "${SERVER}" "sudo systemctl restart portal"

echo "Done!"
