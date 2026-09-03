# Grades Roadmap and Architecture

## Current State

`grades` is a keyboard-driven CLI for managing classes, students, assignments, grading flags, and PowerSchool exports. It uses a local SQLite database and a context-first workflow.

### Recently Completed

| Feature | Status | Notes |
|---------|--------|-------|
| Overview assignment cutoff | Done | `--after`, `--set-after`, `--clear-after` flags on `grades overview` |
| Test speed optimization | Done | Pre-migrated DB template reduces test suite from ~90s to ~25s |
| Test popup fix | Done | `GRADES_NO_OPEN` prevents Windows from opening CSV files during tests |
| Dashboard cutoff indicator | Done | `grades` dashboard shows active overview cutoff |
| PowerShell test helper | Done | `grades-test` function added to profile |
| `grades version` subcommand | Done | Version injected at build time via `-ldflags`; goreleaser config updated |
| Student portal cloud deployment | Done | Go server + SQLite on a VPS behind Caddy/HTTPS; CLI pushes snapshots over HTTP (`grades publish`) |

### Existing Features

- Years, terms, courses, sections, students, categories, assignments
- Multiple grading schemes: raw average, completion/pass-rate
- Grading flags: late, missing, redo, cheat, pass
- Roster CSV import, category CSV import
- PowerSchool-style export with export tracking
- Student portal: auth, grade view, what-if forecasting — deployed on a VPS (HTTPS, SQLite store, teacher admin UI); see [`portal-deployment.md`](portal-deployment.md)
- Database backup and repair tools, including remote backup to the portal server
- Release automation via GitHub Actions + Goreleaser

---

## Architecture

```
┌─────────────────────────────────────────────┐
│  Teacher's computer                          │
│  ├── grades CLI (Cobra + SQLite)            │
│  └── SQLite database (~/.grades/grades.db)  │
└─────────────────────────────────────────────┘
              │
              ▼ grades publish (HTTPS POST /api/admin/publish)
┌─────────────────────────────────────────────┐
│  VPS (grades.mrpopovici.com)                      │
│  ├── Caddy (Let's Encrypt HTTPS)            │
│  ├── portal server (cmd/portal, SQLite)     │
│  └── React SPA (portal-web/dist)            │
└─────────────────────────────────────────────┘
```

### Key Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/` | Cobra CLI wiring |
| `cmd/portal/` | Portal server entrypoint |
| `internal/app/` | Business logic: grades, students, assignments, context, web portal, reports |
| `internal/portalserver/` | Portal HTTP server: auth, JSON API, static SPA |
| `internal/db/` | SQLite connection with foreign keys enforced |
| `internal/migrate/` | Schema migrations (13 versions) |
| `internal/excelreport/` | Python-based Excel report generation |

---

## Short-Term Plans (Next)

### 1. Update Command

`grades update` should check for newer versions and self-install.

- ~~Add `version` subcommand (injected at build time via `ldflags`)~~ — done
- ~~Update `.goreleaser.yaml` to inject version at build time~~ — done
- Host a `version.json` on a China-friendly mirror (Gitee or Tencent COS)
- Download and replace `grades.exe` in-place on Windows

### 2. Deploy Existing Portal — Done

The portal is live on a VPS behind Caddy with automatic HTTPS:

- Server: `cmd/portal` (Go + SQLite), managed by systemd (`scripts/portal.service`)
- Frontend: React SPA built from `portal-web/`, served by the portal binary
- Data sync: `grades publish` / `grades export` push the course snapshot over HTTP (`POST /api/admin/publish`, bearer-token auth) — no SSH or file uploads involved
- Accounts: managed on the laptop (`grades web accounts init|list|reset`), pushed with each publish
- Admin UI at `/admin` (course list, per-student password reset, unpublish)
- Deployments: `scripts/deploy.sh`; setup: `scripts/server-setup.sh`

Details: [`portal-deployment.md`](portal-deployment.md)

---

## Medium-Term Plans (After Portal)

### 3. Student Code Submission Portal

Students upload programming assignments. Server runs test cases and returns results.

**Requires server-side compute:**
- File upload endpoint
- Docker container per submission (CPU/mem limits, timeout, no network)
- Test runner that executes student code against teacher-defined test cases
- Result storage and display

**Architecture:**
- Same single server handles both grade portal and submission runner
- Submission queue: simple in-memory or SQLite-backed queue
- Docker sandbox: one container per run, destroyed after

---

## Long-Term Ideas

| Idea | Complexity | Value |
|------|-----------|-------|
| WeChat Mini Program | High | High distribution in China |
| WeChat Work bot integration | Medium | Push grades directly to class groups |
| Multiple teachers per course | Medium | Shared database, role-based access |
| Parent portal | Low | Subset of student view |
| Mobile app | High | Probably not worth it |

---

## WeChat Mini Program Considerations

A WeChat Mini Program (微信小程序) is a viable alternative to a web portal in China.

### How it would work
- Students open WeChat, scan a QR code or search for the mini program
- Mini Program frontend (WXML/WXSS/JS) runs inside WeChat
- Backend API (on your server) provides: login, grades, submission upload, test results
- The backend is the same server you'd use for a web portal

### Pros
- Every Chinese student has WeChat
- No need to bookmark a URL or remember a password (WeChat handles identity)
- Easy distribution via QR code in class
- Feels native to Chinese users

### Cons
- Requires a **WeChat developer account** (personal accounts limited, business accounts need Chinese business registration)
- Must rewrite the entire frontend in Mini Program framework (can't reuse existing HTML/JS)
- Must register your API domain in WeChat's backend
- Code review process for every update (1-3 days)
- Cannot run arbitrary code client-side (autograder must be server-side anyway)

### Simpler WeChat Alternative
Instead of a Mini Program, just share the web portal URL in a **WeChat group** or **WeChat Official Account menu**. Students click the link and open it in WeChat's built-in browser. This requires zero WeChat bureaucracy and works immediately.

### Recommendation
**Start with the web portal.** If students use it heavily and you want better WeChat integration later, wrap it in a Mini Program. Don't build the Mini Program first — it's extra complexity for the same backend.
