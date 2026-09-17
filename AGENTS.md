# AGENTS.md

Guidance for AI agents and contributors working in this repository.

## What this is

`grades` is a teacher's gradebook tool:

- **CLI** (`main.go`, `cmd/`, `internal/app`, `internal/db`): interactive terminal app for tracking grades, terms, and course years. SQLite via `modernc.org/sqlite` (pure Go, no CGO).
- **Portal server** (`cmd/portal`, `internal/portalserver`): HTTP server students use to see grades, download materials, and upload code submissions (Python/Java, graded by harnesses, JPlag plagiarism checks).
- **Portal frontend** (`portal-web`): React 19 + Vite + Tailwind SPA, built to `portal-web/dist` and served as static files by the portal server.

## Workflow rules

1. **Every change lands via a pull request.** Never push directly to `master`. Branch from `master`, open a PR, wait for CI and the CodeRabbit review, then merge.
2. **CI must be green before merge.** CI runs gofmt, `go vet`, `go test -race`, frontend lint + build, and a full build.
3. Merging to `master` **auto-deploys the portal to production**. Treat every merge as a release to the live site.
4. Versioned releases are cut with tags (`git tag v0.2.0 && git push origin v0.2.0`); see `docs/releasing.md`.

## Verify before opening a PR

```bash
gofmt -l cmd internal main.go        # must print nothing
go vet ./cmd/... ./internal/... .
go test -race ./cmd/... ./internal/... .
go build ./cmd/... .

cd portal-web
npm ci
npm run lint
npm run build
```

Notes:

- Some portal tests need `javac`/`python3` on PATH; they skip when the tool is missing, so a local pass can hide a CI failure. Install a JDK to exercise the Java grader tests.
- Scope Go commands to `./cmd/... ./internal/... .` — `portal-web/node_modules` contains a Go package that must not be scanned.

## Deploying

`./scripts/deploy.sh` builds the frontend + portal binary and deploys to the VPS over SSH as the unprivileged `portal` user, with an atomic binary swap and automatic rollback on health-check failure. It runs automatically on every merge to `master` and can still be run manually.
