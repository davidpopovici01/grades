# Workflow Guide

This guide shows the intended teacher workflow from setup through export.

## 1. Create Or Select Context

The CLI works best when you set context first.

Show the dashboard:

```powershell
grades
```

Set the active year, term, and course:

```powershell
grades context use year 2026-27
grades context use term "Fall 2026"
grades context use course 1
```

Optional:

```powershell
grades context use section 12A
grades context use assignment HW1
```

### Multiple Classes

If you teach more than one class, each course keeps its own saved context. When you switch courses, the previous course's section and assignment are remembered and the new course's last-used context is restored automatically.

```powershell
grades context use course APCSA
grades context use section 12A
grades context use assignment HW1
# APCSA's context is saved automatically

grades context use course APCSP
# APCSP's last section/assignment are restored
```

Saved profiles live in `~/.grades/contexts/<Course>.yaml`. Shared settings such as portal configuration stay in `~/.grades/config.yaml`.

You can list and clean up profiles:

```powershell
grades context profiles
grades context forget APCSP
```

For even faster switching, create shell aliases that pre-load a class:

```powershell
# PowerShell
function ga { grades --class APCSA @args }
function gp { grades --class APCSP @args }
```

```bash
# Bash / zsh
alias ga='grades --class APCSA'
alias gp='grades --class APCSP'
```

Then `ga enter`, `gp show`, etc. work exactly like `grades` but start in the right class. You can also set the `GRADES_CONTEXT` environment variable instead of `--class`.

## 2. Set Up The Class

### Guided Setup

```powershell
grades setup
```

Use this when starting a new course-year from scratch.

### CSV Setup

Roster:

```powershell
grades import
```

Categories:

```powershell
grades categories import
```

## 3. Configure Categories

List categories:

```powershell
grades categories list
```

Set weights:

```powershell
grades categories set-weight Homework 40
grades categories set-weight Exam 60
```

Set schemes:

```powershell
grades categories set-scheme Homework completion
grades categories set-scheme Exam average
```

Set default pass rates:

```powershell
grades categories pass-rate Homework 80
grades categories pass-rate Exam raw
```

`raw` means the category uses raw numeric grades rather than pass/fail-style completion scoring.

## 4. Create Assignments

```powershell
grades assignments add
```

The assignment prompt asks for:

- title
- max score
- category
- pass rate

When an assignment is created, the CLI switches to it automatically.

## 5. Enter Grades

Standard mode:

```powershell
grades enter
```

Ordered by last name:

```powershell
grades enter -lastname
```

Useful inputs:

- `8`
- `19r`
- `l`
- `p` or `pass`
- `f` or `fail`
- `cheat`

Examples:

- `l` marks a scoreless late entry
- `19r` stores a score and forces redo
- `pass` stores a pass result
- `fail` stores a redo-required result with no score
- `cheat` stores a locked zero that cannot be overwritten through normal grade entry

## 6. Review Progress

Current assignment:

```powershell
grades show
```

Whole course or section:

```powershell
grades gradebook
grades overview
grades categories totals
```

### Mid-Semester Cutoff

If you want `grades overview` to ignore older assignments (for example, after a midterm):

```powershell
grades overview --set-after 12
```

This persists the cutoff for the current course and term. From then on, `grades overview` only checks assignments with an ID greater than `12`.

To clear it later:

```powershell
grades overview --clear-after
```

To override the persisted cutoff for a single run:

```powershell
grades overview --after 15
```

Student detail:

```powershell
grades students show Noah
```

The student view includes:

- weighted total at the top
- category totals
- assignment list
- category per assignment
- flags
- `Counts As` percentage

## 7. Manage Flags

Bulk late conversion:

```powershell
grades mark-late
```

Undo bulk late conversion:

```powershell
grades mark-late -undo
```

Clear individual flags:

```powershell
grades clear-late Noah
grades clear-redo Noah
grades clear-cheat Noah
```

## 8. Export Grades

Export the current assignment:

```powershell
grades assignments export
```

Batch export all pending assignments:

```powershell
grades export
```

Or:

```powershell
grades assignments export -all
```

The export system tracks:

- assignments never exported
- assignments changed since the last confirmed export

After each export, the CLI asks:

```text
Was the export successful? [y/N]
```

Only a confirmed export is marked complete.

## 9. Backup And Repair

Backup:

```powershell
grades system db backup
```

Audit repairable legacy rows:

```powershell
grades system repair audit
```

Apply repairs:

```powershell
grades system repair apply
```
