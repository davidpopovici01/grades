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

### Existing Features

- Years, terms, courses, sections, students, categories, assignments
- Multiple grading schemes: raw average, completion/pass-rate
- Grading flags: late, missing, redo, cheat, pass
- Roster CSV import, category CSV import
- PowerSchool-style export with export tracking
- Student portal (local-only): auth, grade view, what-if forecasting
- Database backup and repair tools
- Release automation via GitHub Actions + Goreleaser

---

## Architecture

```
┌─────────────────────────────────────────────┐
│  Teacher's computer                          │
│  ├── grades CLI (Cobra + SQLite)            │
│  ├── SQLite database (~/.grades/grades.db)  │
│  └── grades.exe                             │
└─────────────────────────────────────────────┘
              │
              ▼ publish / deploy
┌─────────────────────────────────────────────┐
│  Student-facing (future)                     │
│  ├── Static JSON snapshots (gradesPublish)  │
│  ├── Web portal (local 127.0.0.1:8080)      │
│  └── Cloud server (planned Phase 2)         │
└─────────────────────────────────────────────┘
```

### Key Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/` | Cobra CLI wiring |
| `internal/app/` | Business logic: grades, students, assignments, context, web portal, reports |
| `internal/db/` | SQLite connection with foreign keys enforced |
| `internal/migrate/` | Schema migrations (10 versions) |
| `internal/excelreport/` | Python-based Excel report generation |

---

## Short-Term Plans (Next)

### 1. Update Command

`grades update` should check for newer versions and self-install.

- Add `version` subcommand (injected at build time via `ldflags`)
- Host a `version.json` on a China-friendly mirror (Gitee or Tencent COS)
- Download and replace `grades.exe` in-place on Windows
- Update `.goreleaser.yaml` to inject version at build time

### 2. Deploy Existing Portal

The existing `portalServer` (`internal/app/web.go`) already supports:
- Student login with passwords
- Grade viewing
- What-if grade forecasting (client-side JS)

**To make it accessible:**
- Bind to `0.0.0.0` instead of `127.0.0.1`
- Add HTTPS (Let's Encrypt or reverse proxy)
- Add `grades cloud-publish` to push snapshots to the server
- Deploy to a single cloud server (Tencent Lighthouse recommended)

**Hosting decision:**
- Mainland China + public IP = cheapest, fastest, no ICP备案 needed
- Hong Kong = 2-3x more expensive
- Friend's server = free if available and proven to work in China

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
