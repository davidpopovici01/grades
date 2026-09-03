#!/bin/bash
set -e

DOMAIN="${1:-grades.mrpopovici.com}"

echo "=== Setting up $DOMAIN portal ==="

# Create portal user and directories
sudo useradd -r -s /bin/false portal 2>/dev/null || true
sudo mkdir -p /opt/portal/static
sudo chown -R portal:portal /opt/portal

# Generate JWT secret (keep an existing one on re-run)
if [ ! -f /opt/portal/.jwt-secret ]; then
    sudo openssl rand -base64 32 | sudo tee /opt/portal/.jwt-secret > /dev/null
    sudo chmod 600 /opt/portal/.jwt-secret
    sudo chown portal:portal /opt/portal/.jwt-secret
else
    echo "Keeping existing JWT secret at /opt/portal/.jwt-secret"
fi

# Generate teacher token (admin bearer token used by the CLI and the /admin UI)
if [ ! -f /opt/portal/.teacher-token ]; then
    TEACHER_TOKEN="$(openssl rand -base64 32)"
    echo "$TEACHER_TOKEN" | sudo tee /opt/portal/.teacher-token > /dev/null
    sudo chmod 600 /opt/portal/.teacher-token
    sudo chown portal:portal /opt/portal/.teacher-token
else
    TEACHER_TOKEN="$(sudo cat /opt/portal/.teacher-token)"
    echo "Keeping existing teacher token at /opt/portal/.teacher-token"
fi

# Caddy: only manage the main Caddyfile when there is no existing config.
# A server that already hosts sites keeps its own Caddy setup untouched.
if [ -f /etc/caddy/Caddyfile ]; then
    echo ""
    echo "Existing /etc/caddy/Caddyfile found — leaving your Caddy setup untouched."
    sudo tee /etc/caddy/portal.caddy-snippet > /dev/null << EOF
$DOMAIN {
    reverse_proxy localhost:8080
}
EOF
    echo "Wrote the portal site block to /etc/caddy/portal.caddy-snippet"
    echo "Add it to your existing Caddy config yourself, e.g. add this line to your Caddyfile:"
    echo "    import /etc/caddy/portal.caddy-snippet"
    echo "then reload Caddy (systemctl reload caddy, or reload/restart your Caddy container)."
else
    if ! command -v caddy &> /dev/null; then
        echo "Installing Caddy..."
        sudo apt update
        sudo apt install -y caddy
    fi
    sudo tee /etc/caddy/Caddyfile << EOF
$DOMAIN {
    reverse_proxy localhost:8080
}
EOF
    if ! sudo systemctl restart caddy; then
        echo "Warning: 'systemctl restart caddy' failed. If another web server or a"
        echo "Dockerized Caddy already owns ports 80/443, keep using that instead and"
        echo "disable this one:  sudo systemctl disable --now caddy"
    fi
fi

echo ""
echo "=== Server setup complete ==="
echo ""
echo "Teacher token (save this): $TEACHER_TOKEN"
echo ""
echo "Next steps:"
echo "  1. Make sure $DOMAIN has an A record pointing at this server"
echo "  2. Deploy the code from your laptop:  ./scripts/deploy.sh"
echo "  3. Start the service:                 sudo systemctl enable --now portal"
echo "  4. On your laptop, add to ~/.grades/config.yaml:"
echo "       portal:"
echo "         url: https://$DOMAIN"
echo "         teacher_token: $TEACHER_TOKEN"
echo "  5. Publish grades from your laptop:   grades publish"
