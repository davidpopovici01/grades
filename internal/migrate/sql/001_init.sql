-- 001_init.sql
-- Initial schema for Grades CLI (SQLite)

-- Track applied migrations
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

-- STUDENTS
CREATE TABLE IF NOT EXISTS students (
  student_pk        INTEGER PRIMARY KEY AUTOINCREMENT,
  first_name        TEXT NOT NULL,
  last_name         TEXT NOT NULL,
  chinese_name      TEXT,
  powerschool_num   TEXT UNIQUE,
  school_student_id TEXT UNIQUE,
  grad_year         INTEGER, -- nullable
  status            TEXT NOT NULL DEFAULT 'active', -- active|inactive|alumni
  status_effective  TEXT, -- date as ISO text
  status_reason     TEXT,
  created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_students_last_first ON students(last_name, first_name);

-- TERMS
CREATE TABLE IF NOT EXISTS terms (
  term_id     INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE, -- e.g., 2025-26 S1
  start_date  TEXT NOT NULL,        -- ISO date
  end_date    TEXT NOT NULL,        -- ISO date
  status      TEXT NOT NULL DEFAULT 'open' -- open|closed
);

CREATE INDEX IF NOT EXISTS idx_terms_dates ON terms(start_date, end_date);

-- CATEGORIES
CREATE TABLE IF NOT EXISTS categories (
  category_id INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE -- HW/Quiz/Test/Project/Attendance
);

-- CATEGORY_SCHEMES
CREATE TABLE IF NOT EXISTS category_schemes (
  scheme_id  INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  notes      TEXT,
  locked_at  TEXT -- nullable; non-null => locked
);

-- CATEGORY_SCHEME_WEIGHTS
CREATE TABLE IF NOT EXISTS category_scheme_weights (
  scheme_id       INTEGER NOT NULL,
  category_id     INTEGER NOT NULL,
  weight_percent  REAL NOT NULL CHECK (weight_percent >= 0 AND weight_percent <= 100),
  PRIMARY KEY (scheme_id, category_id),
  FOREIGN KEY (scheme_id) REFERENCES category_schemes(scheme_id) ON DELETE CASCADE,
  FOREIGN KEY (category_id) REFERENCES categories(category_id) ON DELETE RESTRICT
);

-- COURSES
CREATE TABLE IF NOT EXISTS courses (
  course_id INTEGER PRIMARY KEY AUTOINCREMENT,
  name      TEXT NOT NULL UNIQUE, -- e.g., APCSA, APCSP, Club
  description TEXT
);

-- COURSE_YEARS (your renamed “offerings” concept)
CREATE TABLE IF NOT EXISTS course_years (
  course_year_id INTEGER PRIMARY KEY AUTOINCREMENT,
  course_id      INTEGER NOT NULL,
  name           TEXT NOT NULL, -- e.g., APCSA 2025-26 (or Club 2025-26)
  FOREIGN KEY (course_id) REFERENCES courses(course_id) ON DELETE CASCADE,
  UNIQUE(course_id, name)
);

CREATE INDEX IF NOT EXISTS idx_course_years_course ON course_years(course_id);

-- COURSE_YEAR_TERMS (weights scheme per course-year per term; can be locked)
CREATE TABLE IF NOT EXISTS course_year_terms (
  course_year_id INTEGER NOT NULL,
  term_id        INTEGER NOT NULL,
  scheme_id      INTEGER, -- nullable for things like “attendance only” until you decide
  locked_at      TEXT,    -- nullable; lock the course+term policy
  PRIMARY KEY (course_year_id, term_id),
  FOREIGN KEY (course_year_id) REFERENCES course_years(course_year_id) ON DELETE CASCADE,
  FOREIGN KEY (term_id) REFERENCES terms(term_id) ON DELETE CASCADE,
  FOREIGN KEY (scheme_id) REFERENCES category_schemes(scheme_id) ON DELETE SET NULL
);

-- SECTIONS (APCSA2/APCSA3 etc.)
CREATE TABLE IF NOT EXISTS sections (
  section_id     INTEGER PRIMARY KEY AUTOINCREMENT,
  course_year_id INTEGER NOT NULL,
  name           TEXT NOT NULL, -- e.g., APCSA2
  teacher_name   TEXT,
  FOREIGN KEY (course_year_id) REFERENCES course_years(course_year_id) ON DELETE CASCADE,
  UNIQUE(course_year_id, name)
);

CREATE INDEX IF NOT EXISTS idx_sections_course_year ON sections(course_year_id);

-- SECTION_ENROLLMENTS
CREATE TABLE IF NOT EXISTS section_enrollments (
  section_id INTEGER NOT NULL,
  student_pk INTEGER NOT NULL,
  term_id    INTEGER NOT NULL,
  start_date TEXT NOT NULL,
  end_date   TEXT, -- NULL=active
  status     TEXT NOT NULL DEFAULT 'active', -- active/dropped/moved
  PRIMARY KEY (section_id, student_pk, term_id),
  FOREIGN KEY (section_id) REFERENCES sections(section_id) ON DELETE CASCADE,
  FOREIGN KEY (student_pk) REFERENCES students(student_pk) ON DELETE CASCADE,
  FOREIGN KEY (term_id) REFERENCES terms(term_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_enrollments_student ON section_enrollments(student_pk);
CREATE INDEX IF NOT EXISTS idx_enrollments_section ON section_enrollments(section_id);

-- ASSIGNMENTS
CREATE TABLE IF NOT EXISTS assignments (
  assignment_id  INTEGER PRIMARY KEY AUTOINCREMENT,
  course_year_id INTEGER NOT NULL,
  term_id        INTEGER NOT NULL,
  category_id    INTEGER NOT NULL,
  title          TEXT NOT NULL,
  assigned_date  TEXT,
  max_points     INTEGER NOT NULL CHECK (max_points > 0),
  FOREIGN KEY (course_year_id) REFERENCES course_years(course_year_id) ON DELETE CASCADE,
  FOREIGN KEY (term_id) REFERENCES terms(term_id) ON DELETE CASCADE,
  FOREIGN KEY (category_id) REFERENCES categories(category_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_assignments_course_term ON assignments(course_year_id, term_id);
CREATE INDEX IF NOT EXISTS idx_assignments_term ON assignments(term_id);

-- GRADES
CREATE TABLE IF NOT EXISTS grades (
  assignment_id INTEGER NOT NULL,
  student_pk    INTEGER NOT NULL,
  score         REAL, -- nullable
  flags_bitmask INTEGER NOT NULL DEFAULT 0,
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (assignment_id, student_pk),
  FOREIGN KEY (assignment_id) REFERENCES assignments(assignment_id) ON DELETE CASCADE,
  FOREIGN KEY (student_pk) REFERENCES students(student_pk) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_grades_student ON grades(student_pk);

-- GRADE_AUDIT
CREATE TABLE IF NOT EXISTS grade_audit (
  audit_id      INTEGER PRIMARY KEY AUTOINCREMENT,
  assignment_id INTEGER NOT NULL,
  student_pk    INTEGER NOT NULL,
  action        TEXT NOT NULL, -- INSERT|UPDATE|DELETE
  old_score     REAL,
  new_score     REAL,
  old_flags     INTEGER,
  new_flags     INTEGER,
  changed_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  changed_by    TEXT,
  reason        TEXT,
  FOREIGN KEY (assignment_id) REFERENCES assignments(assignment_id) ON DELETE CASCADE,
  FOREIGN KEY (student_pk) REFERENCES students(student_pk) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_audit_assignment_student ON grade_audit(assignment_id, student_pk);

