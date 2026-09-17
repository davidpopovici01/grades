# Releasing And Packaging

This repository uses pull-request-based development with automated CI, tagged releases, and auto-deploy of the portal.

## Day-To-Day Workflow

1. Branch from `master`, make your change, push the branch.
2. Open a pull request. CI runs gofmt, `go vet`, `go test -race`, the frontend lint + build, and a full build. CodeRabbit reviews the PR automatically.
3. Merge when green. **Merging to `master` auto-deploys the portal to production** (see below).

Direct pushes to `master` are blocked by branch protection.

## CI

On pushes and pull requests to `master`, GitHub Actions runs three jobs:

- **go** — gofmt check, `go vet`, `go test -race` (the runner has a JDK, so the Java grader tests run for real)
- **frontend** — `npm ci`, `npm run lint`, `npm run build` in `portal-web`
- **build** — `go build` of the CLI and portal binaries

On a push to `master` (i.e. a merged PR), a fourth job deploys the portal.

## Auto-Deploy

The `deploy` job runs `scripts/deploy.sh`, which:

- builds the frontend and the linux/amd64 portal binary (version-stamped from git)
- uploads over SSH as the unprivileged `portal` user (never root)
- swaps the binary atomically, restarts `portal.service`, and probes `https://grades.mrpopovici.com/api/health`
- **rolls back to the previous binary automatically** if the health check fails

One-time setup for this to work:

1. Generate a dedicated keypair: `ssh-keygen -t ed25519 -f github-deploy`
2. On the VPS, run the updated `scripts/server-setup.sh` (installs a narrow sudoers rule), then add `github-deploy.pub` to `/home/portal/.ssh/authorized_keys`
3. In the repo: create a GitHub **Environment** named `production` and add the private key as its secret `DEPLOY_SSH_KEY`
4. Add a repo **variable** `DEPLOY_KNOWN_HOSTS` containing the output of `ssh-keyscan 185.223.207.226` (run from a machine that already trusts the server)

You can still deploy from your own machine with `./scripts/deploy.sh` — same script, same safety behavior.

## Tagged Releases

Push a semantic version tag:

```powershell
git switch master
git pull
git tag v0.2.0
git push origin v0.2.0
```

GitHub Actions runs Goreleaser and publishes:

- **CLI archives** — Windows, macOS, Linux (`amd64`/`arm64`), binary named `grades`
- **Portal archive** — `grades-portal_<version>_linux_amd64.tar.gz`: the `portal` server binary plus `static/` (the built frontend), laid out like the deploy target
- checksums for everything

## Local Dry Run

```powershell
goreleaser release --snapshot --clean
```

This builds all artifacts locally without publishing a GitHub release. The frontend must be built first (`cd portal-web && npm ci && npm run build`) so the portal archive can include `static/`.

## Dependency Updates

Dependabot opens weekly PRs for Go modules, npm packages, and GitHub Actions. Actions in workflows are pinned to commit SHAs; Dependabot bumps them like any other dependency.

## Future Packaging Options

- Scoop manifest for Windows
- Homebrew tap for macOS
- package-manager-specific Linux distribution packages
- cosign signing + SBOM for release artifacts
