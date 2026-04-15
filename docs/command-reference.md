# Command Reference

This is a concise reference for the current command structure.

## Top-Level

```text
grades
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
grades clear-late
grades clear-redo
grades clear-cheat
grades gradebook
grades overview
grades stats ...
grades system ...
grades import ...
grades export
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
grades categories import [file]
grades categories setup-csv [file]
grades categories scores
grades categories totals
```

## Assignments

```powershell
grades assignments add
grades assignments list
grades assignments show [assignment-id]
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
grades clear-late [student]
grades clear-redo [student]
grades clear-cheat [student]
```

## Reporting

```powershell
grades gradebook
grades overview
grades stats assignment
grades stats section
grades stats student <student-id>
```

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
```

Exports every assignment in the current course and term that still needs export confirmation.

## System

```powershell
grades system db reset
grades system db backup [file]
grades system migrate up
grades system migrate down
grades system repair audit
grades system repair apply
```

## Legacy Aliases

Some older command locations still work for compatibility, but the commands above are the current documented structure.
