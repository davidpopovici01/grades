# Import And Export Guide

This guide covers all supported CSV import and export workflows.

## Roster Import

### Wizard

```powershell
grades import
```

The wizard can:

- import the default roster file
- create and open the default roster file
- pick another file

If the target file does not exist, the CLI tells you and creates it.

If the default file is chosen for creation, it is always overwritten.

### Roster CSV Format

```csv
year,course,section,student_id,first_name,last_name,chinese_name
2026-27,APCSA,12A,3001,Alice,Brown,Ai Li Si
```

Fields:

- `year`: required
- `course`: required
- `section`: required
- `student_id`: optional
- `first_name`: required
- `last_name`: required
- `chinese_name`: optional

Notes:

- Terms are not part of the roster CSV.
- The importer enrolls the student into all configured terms for that course-year.
- Comment rows are ignored when the first cell starts with `#`.

### Roster Setup CSV

Create it explicitly:

```powershell
grades import setup-csv
```

The generated file:

- includes one ignored example row
- includes existing students from the database
- opens automatically when possible

## Category Import

### Wizard

```powershell
grades categories import
```

### CSV Format

```csv
category,weight,scheme,pass_rate
Homework,40,completion,80
Exam,60,average,raw
```

Fields:

- `category`: required
- `weight`: required
- `scheme`: required
- `pass_rate`: required

Rules:

- numeric-only category names are rejected
- category matching is case-insensitive
- if a category already exists, the import updates it instead of creating a duplicate

### Category Setup CSV

```powershell
grades categories setup-csv
```

## PowerSchool Student Number Import

```powershell
grades students import-powerschool powerschool.csv
```

This reads the PowerSchool-style CSV and updates `powerschool_num` for matching students.

### Name Matching

The importer tries:

- exact `Last, First`
- parsed `Last, ChineseName First`
- similar-name confirmation

If the CSV contains a row such as:

```text
Hou, Xuantong Schuyler
```

the importer interprets it as:

- last name: `Hou`
- Chinese name: `Xuantong`
- first name: `Schuyler`

If an existing student clearly matches, the importer can also update the stored Chinese name.

### Missing Students

If no existing student matches:

- the CLI asks whether to create a new student
- if you choose yes, it asks which section to enroll them in

This avoids accidental duplicates.

## Assignment Export

### Export The Current Assignment

```powershell
grades assignments export
```

Or specify a file:

```powershell
grades assignments export .\hw1.csv
```

### Export All Pending Assignments

```powershell
grades export
```

Or:

```powershell
grades assignments export -all
```

### Export Tracking

The app records the last confirmed export for each assignment.

An assignment is pending export when:

- it has never been exported
- the exported content has changed since the last confirmed export

After each export, you must confirm success:

```text
Was the export successful? [y/N]
```

If you answer `no`, the assignment remains pending.

## PowerSchool Export Format

The export file uses a PowerSchool-style CSV.

Header example:

```csv
Teacher Name:,David Popovici,
Class:,APCSA,
Assignment Name:,HW1,
Due Date:,2026-03-18,
Points Possible:,100.0,
Extra Points:,0.0,
Score Type:,POINTS,
Student Num,Student Name,Score
```

### Exported Name Format

Student names are exported as:

```text
LastName, ChineseName FirstName
```

Example:

```text
Cai, Anji Angela
```

If there is no Chinese name, the export falls back to:

```text
LastName, FirstName
```

### Exported Score Format

All exported assignments are scaled to `100.0` points in the export file.

This is true for all assignments, not just homework.

#### Completion / Pass-Rate Assignments

The export score is the effective counted percentage.

Examples:

- pass only -> `100.00`
- late pass -> `90.00`
- redo pass -> `90.00`
- late + redo pass -> `80.00`
- below pass threshold -> `0.00`

#### Raw-Score Assignments

The export score is:

```text
(raw_score / actual_max_points) * 100
```

Example:

- assignment max `20`
- student score `17`
- exported score `85.00`

### Zero And Blank Behavior

Active students export as `0.00` when the assignment contributes zero, including:

- missing
- scoreless late
- fail / redo-only
- below pass mark
- raw score of zero

Inactive students are still included in the CSV, but their score cell is left blank.

## Export And Backup Locations

Default assignment exports are written to a sibling directory:

```text
../gradesExports
```

Database backups default to:

```text
../gradesBackups
```
