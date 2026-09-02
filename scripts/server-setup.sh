#!/bin/bash
set -e

echo "=== Setting up cs.lairdmath.com portal ==="

# Create portal user and directories
sudo useradd -r -s /bin/false portal 2>/dev/null || true
sudo mkdir -p /opt/portal/static
sudo chown -R portal:portal /opt/portal

# Generate JWT secret
sudo openssl rand -base64 32 | sudo tee /opt/portal/.jwt-secret > /dev/null
sudo chmod 600 /opt/portal/.jwt-secret
sudo chown portal:portal /opt/portal/.jwt-secret

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

# Install Caddy if not present
if ! command -v caddy &> /dev/null; then
    echo "Installing Caddy..."
    sudo apt update
    sudo apt install -y caddy
fi

# Create Caddyfile
sudo tee /etc/caddy/Caddyfile << 'EOF'
cs.lairdmath.com {
    reverse_proxy localhost:8080
}
EOF

# Restart Caddy
sudo systemctl restart caddy

echo "=== Server setup complete ==="
echo ""
echo "Teacher token (save this): $TEACHER_TOKEN"
echo ""
echo "Next steps:"
echo "  1. Deploy the code from your laptop:  ./scripts/deploy.sh"
echo "  2. Start the service:                 sudo systemctl enable --now portal"
echo "  3. On your laptop, add to ~/.grades/config.yaml:"
echo "       portal:"
echo "         url: https://cs.lairdmath.com"
echo "         teacher_token: $TEACHER_TOKEN"
echo "  4. Publish grades from your laptop:   grades publish"
