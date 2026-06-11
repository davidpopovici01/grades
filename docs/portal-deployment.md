# Portal Deployment Guide

This guide covers deploying the student portal to your Singapore server.

## Overview

The portal consists of:
- **Go binary** (`dist/portal`) — the HTTP server
- **Static files** (`portal-web/dist/`) — the React frontend
- **Data files** (`data/`) — published grade snapshots + accounts

## Option 1: SSH Deployment (Recommended)

SSH gives you full control: start/stop the service, check logs, restart after crashes.

### Prerequisites

- SSH access to your Singapore server
- A domain or subdomain pointing to the server's IP (e.g., `cs.lairdmath.com`)

### Step 1: Build for Linux

On your laptop (in the project root):

```bash
# Build the Go binary for Linux
cd /c/Users/david/grades
GOOS=linux GOARCH=amd64 go build -o dist/portal ./cmd/portal

# Build the frontend
cd portal-web && npm run build && cd ..

# Publish your grades
go run . publish ./data
```

### Step 2: Upload to server

```bash
SERVER="user@your-singapore-server"

# Create directories
ssh $SERVER "sudo mkdir -p /opt/portal/data /opt/portal/static && sudo chown -R \$USER:\$USER /opt/portal"

# Upload binary, static files, and data
rsync -avz --delete dist/portal $SERVER:/opt/portal/
rsync -avz --delete portal-web/dist/ $SERVER:/opt/portal/static/
rsync -avz --delete data/ $SERVER:/opt/portal/data/
```

### Step 3: Generate JWT secret

```bash
ssh $SERVER "openssl rand -base64 32 | sudo tee /opt/portal/.jwt-secret > /dev/null && sudo chmod 600 /opt/portal/.jwt-secret"
```

### Step 4: Create systemd service

Create `portal.service` locally, then upload it:

```bash
scp scripts/portal.service $SERVER:/tmp/portal.service
ssh $SERVER "sudo mv /tmp/portal.service /etc/systemd/system/portal.service && sudo systemctl daemon-reload && sudo systemctl enable portal"
```

Start the service:

```bash
ssh $SERVER "sudo systemctl start portal"
```

Check status:

```bash
ssh $SERVER "sudo systemctl status portal"
```

View logs:

```bash
ssh $SERVER "sudo journalctl -u portal -f"
```

### Step 5: Set up the domain (cs.lairdmath.com)

**DNS Setup:**

1. Log into your DNS provider (wherever you manage `lairdmath.com`)
2. Add an **A record**:
   - Name: `cs`
   - Value: `YOUR_SERVER_IP`
   - TTL: 3600 (or auto)
3. Wait 5-30 minutes for DNS propagation

**Option A: Run directly on port 80/443 (simplest)**

Update the systemd service to use port 80:

```bash
ssh $SERVER "sudo systemctl stop portal"
ssh $SERVER "sudo sed -i 's/PORTAL_ADDR=:8080/PORTAL_ADDR=:80/' /etc/systemd/system/portal.service"
ssh $SERVER "sudo systemctl daemon-reload && sudo systemctl start portal"
```

**Option B: Use Caddy as a reverse proxy (recommended for HTTPS)**

Install Caddy on the server:

```bash
ssh $SERVER "sudo apt install -y caddy"
```

Create a Caddyfile:

```bash
ssh $SERVER "sudo tee /etc/caddy/Caddyfile << 'EOF'
cs.lairdmath.com {
    reverse_proxy localhost:8080
}
EOF"
```

Restart Caddy:

```bash
ssh $SERVER "sudo systemctl restart caddy"
```

Caddy will automatically get an SSL certificate from Let's Encrypt.

### Step 6: Update grades later

Whenever you update grades on your laptop:

```bash
cd /c/Users/david/grades
go run . publish ./data
rsync -avz --delete data/ $SERVER:/opt/portal/data/
# No restart needed — server auto-reloads on next request
```

If you changed the Go binary or frontend:

```bash
GOOS=linux GOARCH=amd64 go build -o dist/portal ./cmd/portal
cd portal-web && npm run build && cd ..
rsync -avz --delete dist/portal $SERVER:/opt/portal/
rsync -avz --delete portal-web/dist/ $SERVER:/opt/portal/static/
ssh $SERVER "sudo systemctl restart portal"
```

---

## Option 2: SFTP-Only Deployment

If your host only gives you SFTP (no SSH shell access), you can still deploy, but with limitations.

### What SFTP can do
- ✅ Upload files
- ❌ Start/stop processes
- ❌ Check logs
- ❌ Restart services

### Step 1: Build locally (same as SSH)

```bash
cd /c/Users/david/grades
GOOS=linux GOARCH=amd64 go build -o dist/portal ./cmd/portal
cd portal-web && npm run build && cd ..
go run . publish ./data
```

### Step 2: Upload via SFTP

Use the provided script:

```bash
./scripts/deploy-sftp.sh
```

Or manually with an SFTP client like FileZilla, WinSCP, or Cyberduck:

| Local Path | Remote Path |
|-----------|-------------|
| `dist/portal` | `/opt/portal/portal` |
| `portal-web/dist/*` | `/opt/portal/static/*` |
| `data/*` | `/opt/portal/data/*` |

### Step 3: Ask your host to start the binary

Since you can't run commands, open a support ticket:

> "Hello, I have uploaded a custom web application to `/opt/portal/`. Please run the binary at `/opt/portal/portal` as a background process on startup. It should listen on port 8080. Here is my systemd service file: [attach scripts/portal.service]"

### Step 4: Set up the domain

Same DNS setup as SSH option:
1. Add A record `cs` → your server IP
2. If your host provides a control panel for domains, you may also need to configure the subdomain there

### Limitations of SFTP-only

- **If the server crashes:** You need to open another support ticket to restart it
- **If you change the Go binary:** You need to ask the host to restart the service
- **If you only update grade data:** The server auto-reloads on the next student request (no restart needed)
- **No log access:** You can't see error messages when things break

**Recommendation:** Ask your host for SSH access. It's standard for VPS hosting and makes everything easier.

---

## Subdomain Setup (cs.lairdmath.com)

### 1. DNS Configuration

Log into wherever you manage `lairdmath.com` DNS (Cloudflare, Namecheap, GoDaddy, etc.):

| Type | Name | Value | TTL |
|------|------|-------|-----|
| A | cs | YOUR_SERVER_IP | Auto/3600 |

Example with `dig` to verify:

```bash
dig cs.lairdmath.com A +short
# Should return your server IP
```

### 2. Server Configuration

**If using Caddy (recommended):**

Caddy handles HTTPS automatically. The `Caddyfile` is:

```
cs.lairdmath.com {
    reverse_proxy localhost:8080
}
```

Students visit: `https://cs.lairdmath.com`

**If running directly on port 80:**

Update `PORTAL_ADDR` to `:80` in the systemd service.

Students visit: `http://cs.lairdmath.com`

**If your host only allows port 8080:**

Students visit: `http://cs.lairdmath.com:8080`

(Not recommended — looks unprofessional and no HTTPS)

### 3. Cookie Domain

If running on a subdomain, you may want to set the cookie domain so it works correctly. Update the systemd service:

```ini
Environment="PORTAL_COOKIE_DOMAIN=cs.lairdmath.com"
```

### 4. Rate Limiting

The portal rate-limits requests per IP (default: 300/min). If many students share a school network (same public IP), raise this limit in the systemd service:

```ini
Environment="PORTAL_RATE_LIMIT=600"
```

Set to `0` to disable rate limiting entirely (not recommended for public servers).

---

## Quick Reference

### Build and deploy (SSH)

```bash
# One-liner build + deploy
./scripts/deploy.sh
```

### Build and deploy (SFTP)

```bash
# Upload files only
./scripts/deploy-sftp.sh
```

### Check server status (SSH)

```bash
ssh user@server "sudo systemctl status portal"
```

### View logs (SSH)

```bash
ssh user@server "sudo journalctl -u portal -f"
```

### Restart server (SSH)

```bash
ssh user@server "sudo systemctl restart portal"
```

---

## Troubleshooting

**"Failed to fetch" in browser:**
- Make sure you're accessing via `http://` not `file://`
- Check if the server is running: `sudo systemctl status portal`

**Port already in use:**
- Kill the old process: `sudo pkill portal`
- Or change `PORTAL_ADDR` to a different port

**Can't log in after updating grades:**
- The server auto-reloads `accounts.json` on the next API request
- If it still fails, check that `data/accounts.json` was uploaded correctly

**Domain not resolving:**
- DNS can take 5-30 minutes to propagate
- Check with: `dig cs.lairdmath.com A +short`
