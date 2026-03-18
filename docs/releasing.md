# Releasing And Packaging

This repository now includes automated CI and tagged releases.

## What Happens Automatically

### CI

On pushes and pull requests to `master`, GitHub Actions runs:

- `go test ./...`

This verifies the CLI and migration suite before merge.

### Release

When you push a semantic version tag such as:

```powershell
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions runs Goreleaser and publishes:

- Windows archives
- macOS archives
- Linux archives
- checksums

The release artifacts are attached to the GitHub Release for that tag.

## Supported Targets

The packaged builds target:

- Windows
  - `amd64`
  - `arm64`
- macOS
  - `amd64`
  - `arm64`
- Linux
  - `amd64`
  - `arm64`

## Output Format

Goreleaser builds a binary named:

```text
grades
```

Archives are generated as:

- `.zip` on Windows
- `.tar.gz` on macOS and Linux

Each release also includes a checksum file.

## Release Process

### 1. Make Sure Main Is Ready

Before tagging:

- merge the PR into `master`
- confirm CI is green
- make sure documentation is up to date

### 2. Create The Tag

```powershell
git switch master
git pull
git tag v0.1.0
git push origin v0.1.0
```

### 3. Wait For Release Automation

The release workflow:

- checks out the code
- sets up Go
- runs Goreleaser
- publishes the archives and checksums

## Local Dry Run

If you want to test the release config locally, install Goreleaser and run:

```powershell
goreleaser release --snapshot --clean
```

This builds the artifacts locally without publishing a GitHub release.

## Future Packaging Options

The current setup publishes release archives, which is the correct first step.

Possible next steps later:

- Scoop manifest for Windows
- Homebrew tap for macOS
- package-manager-specific Linux distribution packages

Those can be added after the archive release process is stable.
