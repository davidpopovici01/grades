#!/bin/bash
set -e

# Deploys the portal to the production VPS.
#
# Runs over SSH as the unprivileged "portal" user, not root: it can only
# write under /opt/portal and restart the service through the narrow sudoers
# rule installed by scripts/server-setup.sh.
#
# The swap is atomic and self-healing: the new binary uploads as portal.new,
# becomes portal with a single mv, and if the post-deploy health check fails
# the previous binary is restored automatically.
#
# portal.service is only copied to /opt/portal as a reference; installing a
# changed unit into /etc/systemd requires root and is a manual step (the
# script warns when they drift).
#
# Env overrides: SERVER, REMOTE_DIR, HEALTH_URL.

SERVER="${SERVER:-portal@185.223.207.226}"
REMOTE_DIR="/opt/portal"
STATIC_DIR="${REMOTE_DIR}/static"
HEALTH_URL="${HEALTH_URL:-https://grades.mrpopovici.com/api/health}"

cd "$(dirname "$0")/.."

echo "Building..."
./scripts/build-portal.sh

echo "Uploading to ${SERVER}..."
rsync -avz --delete portal-web/dist/ "${SERVER}:${STATIC_DIR}/"
rsync -avz dist/portal "${SERVER}:${REMOTE_DIR}/portal.new"
rsync -avz scripts/portal.service "${SERVER}:${REMOTE_DIR}/portal.service"

echo "Activating new binary..."
ssh "${SERVER}" "cd ${REMOTE_DIR} && cp -f portal portal.prev && mv portal.new portal"

echo "Restarting portal service..."
ssh "${SERVER}" "sudo systemctl restart portal"

echo "Waiting for health check at ${HEALTH_URL}..."
healthy=0
for _ in $(seq 1 15); do
    if curl -fsS --max-time 5 "${HEALTH_URL}" > /dev/null 2>&1; then
        healthy=1
        break
    fi
    sleep 2
done

if [ "${healthy}" != "1" ]; then
    echo "Health check failed — rolling back to the previous binary..."
    ssh "${SERVER}" "cd ${REMOTE_DIR} && mv portal.prev portal && sudo systemctl restart portal"
    echo "Rolled back; the site should be running the previous version."
    exit 1
fi

if ! ssh "${SERVER}" "cmp -s ${REMOTE_DIR}/portal.service /etc/systemd/system/portal.service"; then
    echo ""
    echo "NOTE: portal.service differs from the installed systemd unit."
    echo "Review /opt/portal/portal.service on the server, then install it with:"
    echo "  sudo cp /opt/portal/portal.service /etc/systemd/system/portal.service && sudo systemctl daemon-reload"
fi

echo "Deployed successfully: $(curl -fsS "${HEALTH_URL}")"
