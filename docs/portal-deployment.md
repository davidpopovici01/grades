# Portal Deployment Guide

This guide covers deploying the student portal to the VPS (grades.mrpopovici.com, with class materials on materials.mrpopovici.com).

## Overview

The portal consists of:

- **Go binary** (`dist/portal`, from `./cmd/portal`) — the HTTP server and JSON API
- **Static files** (`portal-web/dist/`) — the React frontend (serves both subdomains)
- **SQLite database** (`/opt/portal/grades-portal.db`) — snapshots and accounts live here, written by the server itself
- **Materials directory** (`/opt/portal/materials/`) — per-class documents uploaded by the teacher, stored as plain files under `<courseYearId>-<termId>-<course-slug>/`

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
- A records pointing `grades.mrpopovici.com` **and** `materials.mrpopovici.com` at the server's IP
- Caddy on the VPS (any install — apt, Docker, etc.); it obtains the Let's Encrypt certificate automatically

## One-Time Server Setup

Copy the repo (or at least `scripts/`) to the VPS, then run:

```bash
sudo ./scripts/server-setup.sh                 # defaults to grades.mrpopovici.com + materials.mrpopovici.com
sudo ./scripts/server-setup.sh portal.example.com files.example.com   # or pass your domains
```

The script:

- creates the `portal` system user, `/opt/portal/static`, `/opt/portal/materials`, `/opt/portal/submissions`, and `/opt/portal/lib`
- installs a JDK (Java 25+ required by JPlag 6) and `python3` (needed to compile/run student Java and Python submissions) and downloads a version-pinned, sha256-verified JPlag jar to `/opt/portal/lib/jplag.jar` (plagiarism detection; re-running the setup script replaces the jar when the pinned version changes)
- generates `/opt/portal/.jwt-secret` (session signing) and `/opt/portal/.teacher-token` (admin bearer token), both `chmod 600`, owned by `portal`; existing secrets are kept on re-runs
- if there is **no** existing `/etc/caddy/Caddyfile`: installs Caddy if missing and writes a Caddyfile proxying both domains to `localhost:8080`
- if a Caddyfile **already exists** (the server hosts other sites): leaves it untouched and writes both site blocks to `/etc/caddy/portal.caddy-snippet` — add `import /etc/caddy/portal.caddy-snippet` to your Caddyfile (or paste the blocks into your own Caddy config) and reload Caddy
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
| `PORTAL_COOKIE_DOMAIN` | `mrpopovici.com` | shares the session cookie with sibling subdomains (e.g. materials.*); empty = host-only |
| `PORTAL_RATE_LIMIT` | `300` | requests per minute per IP (`0` disables) |
| `PORTAL_MATERIALS_DIR` | `/opt/portal/materials` | where per-class uploaded documents are stored |
| `PORTAL_SUBMISSIONS_DIR` | `/opt/portal/submissions` | student code submissions, test harnesses, and run workspaces |
| `PORTAL_JPLAG_JAR` | `/opt/portal/lib/jplag.jar` | JPlag jar used for plagiarism detection |
| `PORTAL_DEMO_PASSWORD` | *(commented out)* | optional password for the read-only demo student account |

The unit sets `MemoryMax=1536M` as a safety cap. Test runs and plagiarism checks are serialized through a single-worker queue, so peak usage is one `javac`/`java`/`python3` run (30 s wall / 25 s CPU via `prlimit`; 512 MB address space for Python, 4 GB address space with a 256 MB `-Xmx` heap for Java, which needs the extra virtual space to start) or one JPlag run (`-Xmx384m`) at a time on top of the Go server itself (~60 MB).

### Subdomains

Both `grades.mrpopovici.com` and `materials.mrpopovici.com` proxy to the same portal server. The login cookie is domain-wide (`PORTAL_COOKIE_DOMAIN`), so a student who logs in on one subdomain is logged in on the other for the 24-hour session. On the materials host the app lands directly on the Materials page; both hosts expose every page.

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

### Activity

`/admin/activity` shows how the portal is being used, auto-refreshing every 30 seconds:

- **Online now** — students with a request in the last 5 minutes (presence is tracked via a throttled `last_seen_at` timestamp, at most one write per student per minute)
- **Recent activity** — newest-first feed of logins, failed logins, submissions, staged file uploads, material downloads, and password changes
- **Last seen** — every account sorted by least recent activity, with `never` highlighted so disengaged students stand out

The events live in the `activity_events` table in the portal database (`/opt/portal/grades-portal.db`), created automatically on service start. Access logs in the journal (`journalctl -u portal -f`) also append `user=<username>` whenever a request carries a valid session.

### Class materials

Materials are per-class documents (syllabi, handouts, etc.) that students download from `https://materials.mrpopovici.com` (or the Materials tab on the grades site). Manage them from the admin UI: open `https://grades.mrpopovici.com/admin/materials`, pick a published course, and:

- create **categories** (e.g. "Unit 1", "Unit 2") — click a category name to rename it, use the ↑/↓ buttons to reorder, delete is allowed while the category is empty
- **bulk-upload** several files at once (100 MB per file) into a category or into "General"
- click a file name to rename it, use its "Move to…" dropdown to reorganize, or delete it

Students only see materials for courses they are enrolled in, grouped under your category headings in your chosen order (a course must be published for its materials to become visible). Files live in `/opt/portal/materials/<courseYearId>-<termId>-<course-slug>/` on the VPS — category files under `<category-slug>/`, with display names and ordering stored in a small `_meta.json` in each course directory. Deleting a file there also removes it from the site (categories you create by hand on disk show up automatically); unpublishing a course hides its materials without deleting the files.

### Code submissions

Students submit assignments from the Submissions page. Everything is managed from the admin UI at `/admin/submissions` — submission assignments are standalone and are **not** linked to gradebook assignments; create the matching gradebook entry in the CLI yourself when you're ready to record scores.

Each assignment has a **type** that controls validation, testing, and plagiarism:

| Type | Files | Auto tests | Plagiarism |
|------|-------|-----------|------------|
| Java code | at least one `.java`; other files (report, data, video…) allowed alongside | yes | JPlag `java` |
| Python code | at least one `.py`; other files allowed alongside | yes | JPlag `python3` |
| Text / essay | at least one `.txt` or `.md`; other files allowed alongside | no | JPlag `text` (natural language) |
| Other files | any filenames (`.xlsx`, `.docx`, `.mp4`, …) | no | no |

You always set the **exact filenames** students must upload (write `report.docx/pdf` to let the student pick either extension), per-file and total size limits (defaults 256 KB / 1 MB — the "Other files" form preset raises these to 100 MB / 500 MB for videos), a due date, and a late-penalty percent (default 10%). Students see one upload slot per required file: staged files can be reviewed, downloaded back, or removed, and nothing is recorded until they press **Submit** (the late check happens at that moment). At least one staged file is required — any files not re-staged are carried over from their previous submission, so a one-file fix needs only that file re-uploaded. The **Test** button (30 s cooldown, code assignments only) runs the public tests. Unlimited attempts; history is kept.

Automated tests are **harness files** you upload per assignment, marked **public** (students see the results) or **secret** (admin only). A harness is a single file placed next to the student's files when it runs: for Java, a class with a `main` method (compiled together with the student's code); for Python, a script run with `python3`. It prints one line per check:

```text
PASS: adds two numbers
FAIL: handles empty list
```

The server counts the `PASS:`/`FAIL:` lines. Harnesses can be edited in place from the assignment detail page (**Edit** next to a test — saving overwrites the file immediately). The page also has a **Sample Solution** area: add files there as a reference submission and click **Run Tests on Sample** to execute every test against them synchronously and see the full output — the quickest way to verify a harness before students submit. Sample runs are not recorded and never affect scores. From the assignment detail page you can also **Run all tests** (public + secret) on every student's latest submission — including submissions the student never tested. All student runs are serialized through a single-worker queue with per-run limits (30 s wall / 25 s CPU, no core dumps; 512 MB address space for Python, 4 GB address space with a 256 MB heap for Java), so a flood of submissions just queues up. Public run output (PASS/FAIL lines, compiler errors, stack traces) is shown to the student; secret run output is admin-only. Deleting a test also drops its past runs from all scores and from the student view.

**Plagiarism**: the assignment detail page has a plagiarism check that runs JPlag locally over every student's latest submission and shows a similarity table (pairs above 70% highlighted). Every run uses frequency analysis (code fragments shared by many submissions are downweighted, so idiomatic boilerplate counts less) and subsequence match merging (counters match-splitting obfuscation); Java runs additionally use token normalization (renaming variables/methods no longer hides copying). The Plagiarism Check card also has an optional **base code** area: starter/template files uploaded there are subtracted from every submission before comparing (`-bc`). Submissions are labeled by **username** inside the report, so the viewer shows `john.doe` instead of a numeric id. Nothing is uploaded to third parties. After a finished run, **View Full Report** opens the interactive JPlag report viewer (with side-by-side code comparisons) right on the portal in a new tab — the viewer is extracted from the JPlag jar into `/opt/portal/lib/report-viewer/` at service start and served under the viewer's own root-level pages (`/overview`, `/comparison/…`); the report data itself stays behind admin auth via a short-lived `portal_admin` cookie. **Download** fetches the raw `.jplag` file, which also stays on disk at `/opt/portal/submissions/plag/<run_id>.jplag`.

Requirements on the server (installed by `server-setup.sh`): a JDK (Java 25+ for JPlag 6.3; student submission testing itself works with any modern JDK), `python3`, and the JPlag jar at `/opt/portal/lib/jplag.jar`. If any are missing, submissions still work — test runs are marked `unavailable` and the plagiarism UI reports what's missing.

### Demo account

Setting `PORTAL_DEMO_PASSWORD` (or `PORTAL_DEMO_PASSWORD_FILE`, same pattern as the teacher token) makes the server seed a shared, read-only **demo student account** at startup — useful for showing the portal to prospective teachers without touching real data:

- Logs in through the normal login form with username `demo` and the configured password. Share the password privately; it is not displayed anywhere on the site.
- Sees a synthetic sample course ("Sample APCSA") with a realistic grade snapshot, two sample materials, and one sample submission assignment.
- Cannot change the password or upload/submit/test submissions — all mutations return `403 demo account is read-only`, so one visitor cannot lock out the next.

The demo data uses reserved **negative IDs** (`student_pk = -1`, `course_year_id = -1`, `term_id = -1`) that the CLI's autoincrement IDs can never collide with, and the publish cleanup skips negative-ID rows, so publishing real courses never touches the demo account. The username `demo` is reserved end to end: the CLI never generates it for a student (`portalauth.IsReservedUsername`), and if a legacy account already owns it, the server disables the demo at startup instead of touching the real account. Removing the variable and restarting deletes the demo account, course, snapshot, sample assignment, and sample materials directory, so the shared password stops working immediately.

Enabling or rotating the demo password requires editing the live unit on the VPS (`/etc/systemd/system/portal.service`) and running `sudo systemctl daemon-reload && sudo systemctl restart portal` — `deploy.sh` does not manage the unit.

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

**Test runs show "unavailable" / "javac is not installed on the server":**
- The VPS needs the JDK and Python for running submissions, and the JPlag jar for plagiarism (JPlag 6 requires Java 25+). Re-run `server-setup.sh` — it installs what's missing and swaps the JPlag jar when the pinned version changes. No service restart is needed — the grader checks for the tools on every run.

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
