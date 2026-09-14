#!/bin/bash
set -e

DOMAIN="${1:-grades.mrpopovici.com}"
MATERIALS_DOMAIN="${2:-materials.mrpopovici.com}"

echo "=== Setting up $DOMAIN portal ==="

# Create portal user and directories
sudo useradd -r -s /bin/false portal 2>/dev/null || true
sudo mkdir -p /opt/portal/static /opt/portal/materials /opt/portal/submissions /opt/portal/lib
sudo chown -R portal:portal /opt/portal

# Runtimes for code submissions: the JDK compiles/runs Java submissions and
# hosts JPlag; python3 runs Python submissions. JPlag 6 requires Java 25+.
if ! command -v javac &> /dev/null || ! command -v python3 &> /dev/null; then
    echo "Installing Java JDK and Python 3 (needed to run student submissions)..."
    sudo apt update
    sudo apt install -y openjdk-25-jdk-headless python3 || sudo apt install -y default-jdk-headless python3
fi
JAVA_MAJOR="$(java -version 2>&1 | sed -n 's/.*version "\([0-9]*\).*/\1/p' | head -1)"
if [ "${JAVA_MAJOR:-0}" -lt 25 ] 2>/dev/null; then
    echo ""
    echo "WARNING: JPlag 6.3.0 requires Java 25 or newer, but Java ${JAVA_MAJOR:-none} is installed."
    echo "Student submission testing keeps working, but plagiarism checks will fail until"
    echo "you upgrade Java (e.g. openjdk-25-jdk-headless on Ubuntu 25.04+, or the"
    echo "ppa:openjdk-r/ppa PPA on older releases)."
    echo ""
fi

# JPlag (plagiarism detection), version-pinned and checksum-verified.
JPLAG_VERSION="6.3.0"
JPLAG_SHA256="5f2c21e8b88ed77134effcb3a5a3ab13d188f6a3e16d401387f7479e92db9aa2"
if [ -f /opt/portal/lib/jplag.jar ] && echo "$JPLAG_SHA256  /opt/portal/lib/jplag.jar" | sha256sum -c - &> /dev/null; then
    echo "Keeping existing JPlag jar at /opt/portal/lib/jplag.jar"
else
    echo "Downloading JPlag v$JPLAG_VERSION..."
    curl -sL -o /tmp/jplag.jar "https://github.com/jplag/JPlag/releases/download/v${JPLAG_VERSION}/jplag-${JPLAG_VERSION}-jar-with-dependencies.jar"
    echo "$JPLAG_SHA256  /tmp/jplag.jar" | sha256sum -c -
    sudo mv /tmp/jplag.jar /opt/portal/lib/jplag.jar
    sudo chown portal:portal /opt/portal/lib/jplag.jar
fi

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

$MATERIALS_DOMAIN {
    reverse_proxy localhost:8080
}
EOF
    echo "Wrote the portal site blocks to /etc/caddy/portal.caddy-snippet"
    echo "Add them to your existing Caddy config yourself, e.g. add this line to your Caddyfile:"
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

$MATERIALS_DOMAIN {
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
echo "  1. Make sure $DOMAIN and $MATERIALS_DOMAIN have A records pointing at this server"
echo "  2. Deploy the code from your laptop:  ./scripts/deploy.sh"
echo "  3. Start the service:                 sudo systemctl enable --now portal"
echo "  4. On your laptop, add to ~/.grades/config.yaml:"
echo "       portal:"
echo "         url: https://$DOMAIN"
echo "         teacher_token: $TEACHER_TOKEN"
echo "  5. Publish grades from your laptop:   grades publish"
