#!/bin/bash
set -e

echo "=== Setting up cs.lairdmath.com portal ==="

# Create portal user and directories
sudo useradd -r -s /bin/false portal 2>/dev/null || true
sudo mkdir -p /opt/portal/data /opt/portal/static
sudo chown -R portal:portal /opt/portal

# Generate JWT secret
sudo openssl rand -base64 32 | sudo tee /opt/portal/.jwt-secret > /dev/null
sudo chmod 600 /opt/portal/.jwt-secret
sudo chown portal:portal /opt/portal/.jwt-secret

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
echo "Next: upload dist/portal, portal-web/dist/, and data/ to /opt/portal/"
echo "Then: sudo systemctl start portal"
