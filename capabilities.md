\# Grades CLI – Planned Capabilities



This document describes the \*\*intended end-state functionality\*\* of the Grades CLI.



The goal of the project is to provide a \*\*fast, keyboard-driven grade management tool\*\* for teachers that avoids the friction of spreadsheets or web gradebooks.



The CLI is designed around a \*\*context-based workflow\*\* so that users do not need to repeatedly specify parameters.



---



\# Core Structure



The system mirrors the structure of a school gradebook.



```

Term

&nbsp;└── Course Year

&nbsp;     └── Section

&nbsp;          └── Assignment

&nbsp;               └── Student Grades

```



Users move through this structure by setting a \*\*context\*\*.



Example:



```

grades use term fall-2026

grades use course-year apcsa

grades use section 12A

grades use assignment midterm

```



Once context is set, commands operate \*\*within that scope\*\*.



---



\# Command Overview



\## Context Navigation



View current working context.



```

grades

```



Example output:



```

Grades CLI



Context

&nbsp; Term:        Fall 2026

&nbsp; Course:      AP Computer Science A

&nbsp; Section:     12A

&nbsp; Assignment:  Midterm



Next steps

&nbsp; - grades students list

&nbsp; - grades grades enter

```



Set context:



```

grades use term <term>

grades use course-year <course>

grades use section <section>

grades use assignment <assignment>

```



Clear context:



```

grades clear term

grades clear course-year

grades clear section

grades clear assignment

```



---



\# Listing Data



List available entities.



```

grades list terms

grades list course-years

grades list sections

grades list assignments

grades list students

```



Example:



```

grades list students

```



Output:



```

1  Tony Zhang

2  Ivy Chen

3  Hanson Li

4  Charlie Wang

```



---



\# Student Management



Manage students in the current section.



```

grades students add

grades students remove

grades students list

grades students show <student-id>

```



Example:



```

grades students add

```



Prompt:



```

First name: Tony

Last name: Zhang

Student ID: 22145

```



---



\# Assignment Management



Create and configure assignments.



```

grades assignments create

grades assignments list

grades assignments show

grades assignments delete

```



Example:



```

grades assignments create

```



Prompt:



```

Title: Midterm Exam

Max score: 100

Weight: 25%

```



---



\# Grade Entry



Grades are entered through a \*\*fast interactive interface\*\*.



```

grades grades enter

```



The interface allows the teacher to quickly locate students by typing \*\*prefixes of their names\*\*.



Example:



```

> vio

Matched: Violetta Zhang

Score: 92

```



```

> vic

Matched: Victoria Zhao

Score: 88

```



Prefix matching should support:



• first name prefixes

• last name prefixes

• combined initials



Example valid searches:



```

vio

zha

vz

vict

```



---



\# Grade Input Shortcuts



The grading interface supports several \*\*fast entry shortcuts\*\*.



\### Normal score



```

92

```



Records score \*\*92\*\*.



---



\### Late submission



```

92l

```



Records:



```

score = 92

status = late

```



---



\### Missing work



```

m

```



Records:



```

status = missing

score = 0

```



---



\# Undo Support



The interactive grading interface should support \*\*quick correction of mistakes\*\*.



Undo previous entry:



```

undo

```



or



```

u

```



Behavior:



• removes the last recorded grade

• restores the previous student state

• allows re-entry



Example:



```

> vio

Score: 92



> undo

Previous entry removed.

```



---



\# Viewing Grades



Show grades for the current assignment.



```

grades grades show

```



Example output:



```

Midterm Exam



Tony Zhang      92

Ivy Chen        88

Hanson Li       94

Charlie Wang    M

```



Where:



```

M = Missing

L = Late

```



---



\# Gradebook View



Display a \*\*table view of the entire gradebook\*\*.



```

grades gradebook

```



Example:



```

Student        Midterm   Project   Final

----------------------------------------

Tony Zhang        92        88       94

Ivy Chen          88        91       90

Hanson Li         94        87       93

```



This command allows teachers to quickly inspect \*\*overall class performance\*\*.



---



\# Analytics



Basic statistics for assignments or sections.



```

grades stats assignment

grades stats section

grades stats student

```



Example:



```

grades stats assignment

```



Output:



```

Average: 88.2

Highest: 94

Lowest: 72

```



---



\# Import / Export



Data interoperability commands.



```

grades import students <file>

grades export grades <file>

```



Example:



```

grades export grades midterm.csv

```



---



\# Database Commands



Internal database operations.



```

grades migrate up

grades migrate down

grades db reset

```



These commands are primarily intended for \*\*development and maintenance\*\*.



---



\# Typical Workflow



Example teacher workflow:



```

grades use term fall-2026

grades use course-year apcsa

grades use section 12A



grades assignments create

grades use assignment midterm



grades grades enter

grades stats assignment

```



---



\# Design Philosophy



The Grades CLI follows several guiding principles:



\### Context-First Workflow



Once context is selected, the user should \*\*never need to repeat parameters\*\*.



\### Minimal Typing



Commands and grading workflows should minimize keystrokes.



\### Teacher Speed



The tool should allow teachers to \*\*enter grades faster than spreadsheets or web tools\*\*.



---



\# Future Enhancements



Planned future capabilities include:



• fuzzy name matching

• grade curves

• assignment templates

• comment feedback per student

• PowerSchool export compatibility

• web dashboard integration



