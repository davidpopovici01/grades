# Portal Deployment Guide

This guide covers deploying the student portal to the VPS (grades.mrpopovici.com).

## Overview

The portal consists of:

- **Go binary** (`dist/portal`, from `./cmd/portal`) — the HTTP server and JSON API
- **Static files** (`portal-web/dist/`) — the React frontend
- **SQLite database** (`/opt/portal/grades-portal.db`) — snapshots and accounts live here, written by the server itself

There are no data files to upload. Grade data flows over HTTP:

```text
Laptop (grades CLI)                      VPS
───────────────────                      ───
grades export / grades publish  ──HTTPS──▶  Caddy (Let's Encrypt)
  POST /api/admin/publish                    └── reverse_proxy ──▶ portal server (:8080)
  Authorization: Bearer <token>                    ├── serves portal-web/dist (React SPA)
                                                   ├── /api/* student endpoints (JWT cookie)
                                                   └── /api/admin/* (teacher token)
```

## Prerequisites

- SSH access to the VPS
- An A record pointing `grades.mrpopovici.com` at the server's IP
- Caddy on the VPS (any install — apt, Docker, etc.); it obtains the Let's Encrypt certificate automatically

## One-Time Server Setup

Copy the repo (or at least `scripts/`) to the VPS, then run:

```bash
sudo ./scripts/server-setup.sh                 # defaults to grades.mrpopovici.com
sudo ./scripts/server-setup.sh portal.example.com   # or pass your domain
```

The script:

- creates the `portal` system user and `/opt/portal/static`
- generates `/opt/portal/.jwt-secret` (session signing) and `/opt/portal/.teacher-token` (admin bearer token), both `chmod 600`, owned by `portal`; existing secrets are kept on re-runs
- if there is **no** existing `/etc/caddy/Caddyfile`: installs Caddy if missing and writes a Caddyfile proxying the domain to `localhost:8080`
- if a Caddyfile **already exists** (the server hosts other sites): leaves it untouched and writes the portal site block to `/etc/caddy/portal.caddy-snippet` — add `import /etc/caddy/portal.caddy-snippet` to your Caddyfile (or paste the block into your own Caddy config) and reload Caddy
- prints the teacher token once — save it for the laptop config below

Then install the systemd service:

```bash
scp scripts/portal.service user@server:/tmp/portal.service
ssh user@server "sudo mv /tmp/portal.service /etc/systemd/system/portal.service && sudo systemctl daemon-reload"
```

## Deploying Code

From your laptop (builds the frontend, cross-compiles the binary, rsyncs both, restarts the service):

```bash
./scripts/deploy.sh
```

Override the target with the `SERVER` environment variable (default: `user@singapore-vps`):

```bash
SERVER="user@your-server" ./scripts/deploy.sh
```

Alternative — build on the VPS itself:

```bash
ssh user@server
cd grades && git pull
./scripts/build-portal.sh
sudo cp dist/portal /opt/portal/portal
sudo rsync -a --delete portal-web/dist/ /opt/portal/static/
sudo systemctl restart portal
```

Finally, enable and start the service:

```bash
ssh user@server "sudo systemctl enable --now portal"
```

## The systemd Service

`scripts/portal.service` runs `/opt/portal/portal` as the `portal` user with this environment:

| Variable | Value in portal.service | Purpose |
|----------|------------------------|---------|
| `PORTAL_ADDR` | `:8080` | listen address |
| `PORTAL_STATIC_DIR` | `/opt/portal/static` | React frontend files |
| `PORTAL_DB_PATH` | `/opt/portal/grades-portal.db` | SQLite store (created by the server) |
| `PORTAL_JWT_SECRET_FILE` | `/opt/portal/.jwt-secret` | signs student session cookies |
| `PORTAL_TEACHER_TOKEN_FILE` | `/opt/portal/.teacher-token` | admin bearer token |
| `PORTAL_COOKIE_SECURE` | `true` | HTTPS-only cookies |
| `PORTAL_RATE_LIMIT` | `300` | requests per minute per IP (`0` disables) |

Notes:

- `PORTAL_JWT_SECRET` / `PORTAL_TEACHER_TOKEN` (inline values) are accepted instead of the `*_FILE` variants. A JWT secret is **required** — the server refuses to start without one.
- Without a teacher token the server runs, but all `/api/admin/*` endpoints return 503.
- If many students share one school IP, raise `PORTAL_RATE_LIMIT` (e.g. `600`).

## Laptop Configuration

Add to `~/.grades/config.yaml`:

```yaml
portal:
  url: https://grades.mrpopovici.com
  teacher_token: <token printed by server-setup.sh>
  server: user@your-server        # optional, for backups
  key: ~/.ssh/id_ed25519          # optional, SSH key for backups
  remote_dir: /opt/portal         # optional, default ~/portal
```

- `url` + `teacher_token` — used by `grades publish` and `grades export` to push snapshots
- `server` / `key` / `remote_dir` — used by `grades system db backup-remote` (SSH/rsync)

## Day-To-Day Use

Publish the current course and term to the portal:

```bash
grades publish
```

`grades export` (and `grades assignments export`) push automatically after exporting when `portal.url` is configured — no separate publish step is needed. With no `portal.url` set, `grades publish` skips with a notice; run `grades web serve` for a local preview that reads the database directly.

### Student accounts

Students log in with per-student usernames and passwords. Manage accounts from the CLI:

```bash
grades web accounts init              # create accounts for the current course/term
grades web accounts init -m           # memorable 3-word passwords
grades web accounts list              # show usernames
grades web accounts reset <student>   # reset one password (-p to set it, -m for memorable)
```

Accounts are included in the next publish.

### Admin UI

Open `https://grades.mrpopovici.com/admin` and log in with the teacher token. The dashboard lists published courses; each course shows its students, and you can reset a student's password or unpublish a course from there.

## Backups

The laptop's gradebook database is the source of truth. Copy it to the VPS over SSH:

```bash
grades system db backup-remote
```

This requires `portal.server` (and optionally `portal.key` / `portal.remote_dir`) in the config and writes to `<remote_dir>/backups/grades.db` via rsync. Local backups still work with `grades system db backup`.

## Local Preview

Quick preview without any server setup (embedded page, serves the current course snapshot):

```bash
grades web serve
```

To run the real portal server (React frontend + SQLite + admin API) locally:

```bash
./scripts/run-local.sh        # or scripts\run-local.ps1 on Windows
```

It builds `portal-web/dist` if missing, generates a throwaway JWT secret and teacher token under `dist/portal-local/`, and starts the server on `http://localhost:8080`. Point `portal.url` at `http://localhost:8080` to test publishing against it.

## Service Operations

```bash
ssh user@server "sudo systemctl status portal"     # status
ssh user@server "sudo journalctl -u portal -f"     # logs
ssh user@server "sudo systemctl restart portal"    # restart
```

## Troubleshooting

**`grades publish` fails with 401:**
- `portal.teacher_token` in `~/.grades/config.yaml` must match `/opt/portal/.teacher-token` on the server.

**`grades publish` fails with 503 "admin API not configured":**
- The server has no teacher token. Check that `PORTAL_TEACHER_TOKEN_FILE` points at a readable file and restart the service.

**"Failed to fetch" in the browser:**
- Check the service: `sudo systemctl status portal`
- Check Caddy: `sudo systemctl status caddy`

**Port already in use:**
- Find the old process: `sudo lsof -i :8080`, or change `PORTAL_ADDR` in the service.

**Domain not resolving:**
- DNS can take 5–30 minutes to propagate. Check with: `dig grades.mrpopovici.com A +short`

**Students can't log in after publishing:**
- Accounts are created with `grades web accounts init` on the laptop and pushed with the next publish. Verify with `grades web accounts list`.
