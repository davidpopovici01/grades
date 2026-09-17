#!/bin/bash
set -e

# Deploys the portal to the production VPS.
#
# Runs over SSH as the unprivileged "portal" user, not root: it can only
# write under /opt/portal and restart the service through the narrow sudoers
# rule installed by scripts/server-setup.sh.
#
# The binary and the frontend assets switch as one release: both upload next
# to the live versions and swap in a single step, keeping .prev copies. If the
# restart fails or the post-deploy health check does not pass, both are
# rolled back automatically.
#
# portal.service is only copied to /opt/portal as a reference; installing a
# changed unit into /etc/systemd requires root and is a manual step (the
# script warns when they drift).
#
# Env overrides: SERVER, REMOTE_DIR, HEALTH_URL.

SERVER="${SERVER:-portal@185.223.207.226}"
REMOTE_DIR="/opt/portal"
HEALTH_URL="${HEALTH_URL:-https://grades.mrpopovici.com/api/health}"

cd "$(dirname "$0")/.."

# rollback restores the previous binary and frontend assets, if any exist.
rollback() {
    echo "Rolling back to the previous release..."
    ssh "${SERVER}" "cd ${REMOTE_DIR} && \
        if [ -f portal.prev ]; then mv portal.prev portal; fi && \
        if [ -d static.prev ]; then rm -rf static && mv static.prev static; fi && \
        sudo systemctl restart portal" || true
}

echo "Building..."
./scripts/build-portal.sh

echo "Uploading to ${SERVER}..."
rsync -avz dist/portal "${SERVER}:${REMOTE_DIR}/portal.new"
rsync -avz --delete --delay-updates portal-web/dist/ "${SERVER}:${REMOTE_DIR}/static.new/"
rsync -avz scripts/portal.service "${SERVER}:${REMOTE_DIR}/portal.service"

echo "Activating new release..."
ssh "${SERVER}" "set -e; cd ${REMOTE_DIR}; \
    if [ -f portal ]; then cp -f portal portal.prev; fi; \
    if [ -d static ]; then rm -rf static.prev && mv static static.prev; fi; \
    mv portal.new portal; \
    mv static.new static"

echo "Restarting portal service..."
if ! ssh "${SERVER}" "sudo systemctl restart portal"; then
    echo "Service restart failed."
    rollback
    exit 1
fi

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
    echo "Health check failed."
    rollback
    exit 1
fi

if ! ssh "${SERVER}" "cmp -s ${REMOTE_DIR}/portal.service /etc/systemd/system/portal.service"; then
    echo ""
    echo "NOTE: portal.service differs from the installed systemd unit."
    echo "Review /opt/portal/portal.service on the server, then install it with:"
    echo "  sudo cp /opt/portal/portal.service /etc/systemd/system/portal.service && sudo systemctl daemon-reload"
fi

echo "Deployed successfully: $(curl -fsS "${HEALTH_URL}")"
