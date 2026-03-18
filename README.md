# grades

`grades` is a keyboard-driven CLI for managing classes, students, assignments, grading flags, reporting, and PowerSchool exports.

It is built for a teacher workflow:

- fast context switching
- quick grade entry
- setup from CSV
- grading rules that match real classroom use
- PowerSchool-style export files
- a local SQLite database with backups and repair tools

## What It Does

- Manages years, terms, courses, sections, students, categories, and assignments
- Supports multiple grading models:
  - raw score / average
  - completion / pass-rate based scoring
- Tracks grading flags such as:
  - `late`
  - `redo`
  - `missing`
  - `cheat`
- Imports:
  - roster CSVs
  - category setup CSVs
  - PowerSchool student numbers
- Exports assignment grades in PowerSchool CSV format
- Tracks whether an assignment has been exported, and whether it needs re-export after changes
- Provides:
  - gradebook view
  - overview view
  - student detail view
  - category totals
  - database backup and repair commands

## Install And Run

### Run From Source

```powershell
go run .
```

### PowerShell Wrapper

If you want `grades` to always run the local repo without reinstalling:

```powershell
function grades {
    Push-Location 'C:\Users\david\grades'
    try {
        go run . @Args
    }
    finally {
        Pop-Location
    }
}
```

### Build A Local Binary

```powershell
go build -o grades.exe .
```

### Install From Releases

Tagged releases publish packaged binaries automatically for:

- Windows
- macOS
- Linux

See the repository Releases page and download the archive for your platform.

## Quick Start

### 1. First-Time Setup

```powershell
grades setup
```

This walks through:

- course
- terms
- sections
- students

### 2. Or Import A Roster CSV

```powershell
grades import
```

The import wizard can:

- create or overwrite the default roster template
- open it for editing
- import the default file
- import another file

### 3. Set Context

```powershell
grades context use year 2026-27
grades context use term "Fall 2026"
grades context use course 1
grades context use section 12A
```

### 4. Create Categories And Assignments

```powershell
grades categories list
grades assignments add
```

### 5. Enter Grades

```powershell
grades enter
```

Or ordered by last name:

```powershell
grades enter -lastname
```

### 6. Review Status

```powershell
grades show
grades overview
grades gradebook
```

### 7. Export For PowerSchool

Export the current assignment:

```powershell
grades assignments export
```

Export all assignments in the current course and term that are either:

- never exported
- changed since the last confirmed export

```powershell
grades export
```

You can also batch-export from the assignment command:

```powershell
grades assignments export -all
```

## Core Workflow

The app is context-first. Most commands operate on the currently selected:

- year
- term
- course
- section
- assignment

Show the dashboard:

```powershell
grades
```

Set context:

```powershell
grades context use year 2026-27
grades context use term "Fall 2026"
grades context use course 1
grades context use section 12A
grades context use assignment HW1
```

Clear context:

```powershell
grades context clear section
grades context clear assignment
```

## Daily Commands

### Grade Entry

```powershell
grades enter
grades enter -lastname
grades pass Noah
grades fill pass
grades mark-late
grades mark-late -undo
grades clear-late Noah
grades clear-redo Noah
grades clear-cheat Noah
```

### Views

```powershell
grades show
grades gradebook
grades overview
grades students show Noah
grades categories totals
```

### Student Management

```powershell
grades students add
grades students remove Noah
grades students deactivate Noah
grades students activate Noah
grades students import-powerschool powerschool.csv
```

### Assignment Management

```powershell
grades assignments add
grades assignments list
grades assignments show
grades assignments max 20
grades assignments pass-rate 80
grades assignments curve show
grades assignments curve set 100 5
grades assignments curve target 85
```

### Category Management

```powershell
grades categories list
grades categories set-weight Homework 40
grades categories set-scheme Homework completion
grades categories pass-rate Homework 80
grades categories import
grades categories totals
```

## Import And Export

### Roster Import

The standalone roster CSV format is:

```csv
year,course,section,student_id,first_name,last_name,chinese_name
2026-27,APCSA,12A,3001,Alice,Brown,Ai Li Si
```

Terms are handled separately. The roster import enrolls students into all configured terms for the matching course-year.

### Category Import

The category CSV format is:

```csv
category,weight,scheme,pass_rate
Homework,40,completion,80
Exam,60,average,raw
```

### PowerSchool Export

Assignment export writes a PowerSchool-style CSV and now tracks export state.

Each assignment is considered pending export when:

- it has never been exported
- its export content changed since the last confirmed export

### PowerSchool Student Number Import

You can import a PowerSchool CSV to update student PowerSchool numbers:

```powershell
grades students import-powerschool powerschool.csv
```

If the import finds a similar student instead of an exact match, it asks for confirmation before updating or creating a new student.

## Data Safety

Create a backup:

```powershell
grades system db backup
```

Audit repairable legacy data:

```powershell
grades system repair audit
```

Apply repairs:

```powershell
grades system repair apply
```

## Documentation

Detailed docs live in [`docs/`](docs):

- [`docs/workflow.md`](docs/workflow.md)
- [`docs/import-export.md`](docs/import-export.md)
- [`docs/grading-rules.md`](docs/grading-rules.md)
- [`docs/command-reference.md`](docs/command-reference.md)
- [`docs/releasing.md`](docs/releasing.md)

## Release Automation

This repository includes:

- CI on pushes and pull requests
- automatic tagged releases
- packaged binaries for Windows, macOS, and Linux
- release archives and checksums

To publish a release:

```powershell
git tag v0.1.0
git push origin v0.1.0
```

That tag triggers the release workflow and publishes packaged binaries automatically.
