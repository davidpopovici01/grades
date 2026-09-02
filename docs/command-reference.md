# Command Reference

This is a concise reference for the current command structure.

## Top-Level

```text
grades
grades --class <course>
grades setup
grades context ...
grades students ...
grades categories ...
grades assignments ...
grades enter
grades show
grades pass
grades fill ...
grades mark-late
grades mark-zero-redo
grades mark-zero-late
grades clear-late
grades clear-redo
grades clear-cheat
grades gradebook
grades overview
grades stats ...
grades reports ...
grades excel-report
grades system ...
grades import ...
grades export
grades publish
grades web ...
grades version
```

Use `grades --class <course>` (or the `GRADES_CONTEXT` environment variable) to run any command with a specific course's saved context. This is useful for shell aliases such as `ga='grades --class APCSA'`.

Example:

```powershell
grades --class APCSA enter
$env:GRADES_CONTEXT = "APCSA"; grades enter
```

## Dashboard

```powershell
grades
```

Shows the current context and suggested next steps.

## Context

```powershell
grades context use year <name-or-id>
grades context use term <name>
grades context use course <name-or-id>
grades context use section <name-or-id>
grades context use assignment <title-or-id>
```

Switching courses automatically saves the previous course's context and restores the new course's last-used context. Profiles are stored under `~/.grades/contexts/<Course>.yaml`.

```powershell
grades context profiles
grades context forget <course>
```

```powershell
grades context clear year
grades context clear term
grades context clear course-year
grades context clear section
grades context clear assignment
```

```powershell
grades context list years
grades context list terms
grades context list courses
grades context list sections
grades context list assignments
grades context list students
```

## Students

```powershell
grades students add
grades students remove [student]
grades students list
grades students show <student>
grades students import-powerschool <file>
grades students deactivate [student]
grades students activate [student]
```

## Categories

```powershell
grades categories list
grades categories set-weight <category> <percent>
grades categories weight <category> <percent>
grades categories schemes
grades categories set-scheme <category> <scheme>
grades categories pass-rate <category> <percent|raw>
grades categories show <category>
grades categories hide <category>
grades categories set-visibility <category> <true|false>
grades categories import [file]
grades categories setup-csv [file]
grades categories scores
grades categories totals
```

`show`, `hide`, and `set-visibility` control whether a category appears in the overview.

## Assignments

```powershell
grades assignments add
grades assignments list
grades assignments show [assignment-id]
grades assignments edit
grades assignments delete [assignment-id]
grades assignments max <points>
grades assignments pass-rate <percent|raw|default>
grades assignments export [file]
grades assignments export -all
```

Curve tools:

```powershell
grades assignments curve show
grades assignments curve set <lift>
grades assignments curve target <desired-average>
```

## Grade Entry And Flags

```powershell
grades enter
grades enter -lastname
grades show
grades pass [student]
grades fill pass
grades mark-late
grades mark-late -undo
grades mark-zero-redo
grades mark-zero-late
grades clear-late [student]
grades clear-redo [student]
grades clear-cheat [student]
```

## Outstanding Work (Without Switching Assignments)

```powershell
grades make-up list <student>
grades make-up enter <student>
grades make-up pass <student>
grades redo list <student>
grades redo pass <student>
```

`make-up` now includes any active redo work as well as late/missing assignments, so `make-up pass` can clear a zero-score redo in one step.

## Reporting

```powershell
grades gradebook
grades overview
grades overview --after <assignment-id>
grades overview --set-after <assignment-id>
grades overview --clear-after
grades stats assignment
grades stats section
grades stats student <student-id>
grades reports suggest
grades reports create <student> [file]
grades excel-report [workbook]
```

`excel-report` fills the Senior 2 APCSA Excel report. Flags: `--workbook`, `--sheet` (default `Senior2`), `--teacher`, `--exam-category` (default `Midterm`), `--printable`, `--skip-c-scores`.

Overview cutoff:

- `grades overview --after <id>` — one-time override, only checks assignments with ID greater than `<id>`
- `grades overview --set-after <id>` — persist the cutoff for the current course and term
- `grades overview --clear-after` — remove the persisted cutoff

## Import

```powershell
grades import
grades import students <file>
grades import roster <file>
grades import setup-csv [file]
```

## Export

```powershell
grades export
grades export grades [file]
```

Exports every assignment in the current course and term that still needs export confirmation. When `portal.url` is set in `~/.grades/config.yaml`, export also pushes the course snapshot to the student portal afterwards (same as `grades publish`).

## Student Portal

```powershell
grades publish
grades web serve [addr]
grades web token
grades web accounts init [default-password] [-m]
grades web accounts list
grades web accounts reset <student> [-p <password>] [-m]
```

- `grades publish` pushes the current course and term to the portal over HTTP when `portal.url` is configured; with no `portal.url` it skips with a notice.
- `grades web serve` runs a local preview of the student portal (default `127.0.0.1:8080`), serving the built React app from `portal-web/dist` when available and reading grade data straight from the database.
- `grades web token` prints the admin (teacher) token from your config, for pasting into the portal's `/admin` login page.
- `grades web accounts` manages the local student accounts that are included in each publish.

See [`portal-deployment.md`](portal-deployment.md) for server setup and configuration.

## System

```powershell
grades system db reset
grades system db backup [file]
grades system db backup-remote
grades system migrate up
grades system migrate down
grades system repair audit
grades system repair apply
```

`grades system db backup-remote` copies the database to the portal server over SSH; it needs `portal.server` (and optionally `portal.key` / `portal.remote_dir`) in `~/.grades/config.yaml`.

## Version

```powershell
grades version
```

Shows the CLI version (injected at build time; `dev` for `go run`).

## Legacy Aliases

Some older command locations still work for compatibility, but the commands above are the current documented structure.
