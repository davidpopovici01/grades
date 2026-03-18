# Grading Rules

This document describes how grades, flags, and category totals behave.

## Category Scoring Modes

Each category can use a grading scheme.

Common schemes:

- `average`
- `completion`

## Raw Score Categories

Raw-score categories use the recorded numeric grade directly.

Examples:

- Tests
- Projects
- Major assessments

These categories normally use:

```text
pass_rate = raw
```

## Completion / Pass-Rate Categories

Completion categories use a threshold-based model.

Examples:

- Homework
- Practice
- Quick checks

Each assignment or category can define a pass rate. If no assignment-level pass rate is set, the category default is used.

If neither is defined, the app falls back to the normal default behavior already configured in the grading logic.

## Pass Threshold Logic

For completion-style scoring:

- score below pass threshold -> `0%`
- fail / redo-only -> `0%`
- missing -> `0%`
- scoreless late -> `0%`
- pass at or above threshold -> `100%`

Then penalties apply:

- `late` subtracts `10%`
- `redo` subtracts `10%`

Examples:

- passed, no flags -> `100%`
- passed, late -> `90%`
- passed, redo -> `90%`
- passed, late + redo -> `80%`

## Flags

### Missing

Missing indicates no valid submission.

Effects:

- contributes zero
- appears in overview
- can be bulk-converted to late using `mark-late`

### Late

Late can exist with or without a score.

Rules:

- `mark-late` replaces missing with scoreless late
- `mark-late -undo` converts scoreless late back to missing
- `mark-late` is intended as a bulk conversion

Overview behavior:

- late only appears in overview when there is no score attached

### Redo

Redo means the student must redo the work.

Rules:

- a score below the pass threshold sets redo
- redo is sticky
- a later higher score does not automatically clear redo
- use `clear-redo` to remove it explicitly

Overview behavior:

- redo appears in overview when the score is below the threshold
- if the student has since passed and redo remains stored, overview treats it as OK
- `grades show` still shows the stored flag state

### Cheat

`cheat` is a locked zero.

Rules:

- created by entering `cheat`
- does not show up in overview
- cannot be overwritten through normal grade entry
- must be cleared with `clear-cheat`
- appears in the gradebook as a visually distinct zero

## Grade Entry Inputs

Examples of supported inputs:

- `8`
- `8l`
- `19r`
- `l`
- `p`
- `pass`
- `f`
- `fail`
- `cheat`

Meaning:

- `l` -> scoreless late
- `19r` -> score plus explicit redo
- `pass` -> pass result
- `fail` -> redo-required result with no score
- `cheat` -> locked zero

## Views

### grades show

Shows the current assignment with the actual stored flags.

This is the best view for checking the exact state of an entry.

### grades overview

Shows per-student grouped status for the current term.

Current behavior:

- `Late` only shows when there is no score
- `Redo` shows for below-threshold / active redo work
- otherwise the work is treated as OK in overview

### grades gradebook

Shows the broader table view across assignments.

Important display behaviors:

- colored cells
- chunked output when there are many assignments
- passing numeric grades can be shown in green
- cheat entries are shown distinctly

## Student Status

Students can be:

- `active`
- `inactive`

Inactive students are hidden from normal active workflows such as:

- list
- gradebook
- overview
- grade entry lookup

But they are still handled specially during export.

## Repair Command

The repair command exists to normalize legacy rows that were created under older rules.

Use:

```powershell
grades system repair audit
grades system repair apply
```

It currently repairs cases such as:

- `missing + late` -> late only
- low score with missing redo flag -> add redo
- redo-only zero-score rows -> scoreless redo
