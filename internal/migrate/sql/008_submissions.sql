CREATE TABLE IF NOT EXISTS submission_policies (
  assignment_id         INTEGER PRIMARY KEY,
  enabled               INTEGER NOT NULL DEFAULT 1,
  due_at                TEXT,
  late_allowed          INTEGER NOT NULL DEFAULT 1,
  late_cap_percent      REAL NOT NULL DEFAULT 70 CHECK (late_cap_percent >= 0 AND late_cap_percent <= 100),
  expected_file_count   INTEGER NOT NULL DEFAULT 1,
  expected_filenames    TEXT NOT NULL DEFAULT '',
  instructions          TEXT NOT NULL DEFAULT '',
  updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  FOREIGN KEY (assignment_id) REFERENCES assignments(assignment_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS submission_attempts (
  attempt_id            INTEGER PRIMARY KEY AUTOINCREMENT,
  assignment_id         INTEGER NOT NULL,
  student_pk            INTEGER NOT NULL,
  submitted_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  is_late               INTEGER NOT NULL DEFAULT 0,
  cap_percent           REAL NOT NULL DEFAULT 100,
  FOREIGN KEY (assignment_id) REFERENCES assignments(assignment_id) ON DELETE CASCADE,
  FOREIGN KEY (student_pk) REFERENCES students(student_pk) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_submission_attempts_assignment_student ON submission_attempts(assignment_id, student_pk, submitted_at DESC);

CREATE TABLE IF NOT EXISTS submission_files (
  submission_file_id    INTEGER PRIMARY KEY AUTOINCREMENT,
  attempt_id            INTEGER NOT NULL,
  original_name         TEXT NOT NULL,
  stored_name           TEXT NOT NULL,
  relative_path         TEXT NOT NULL,
  byte_size             INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY (attempt_id) REFERENCES submission_attempts(attempt_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_submission_files_attempt ON submission_files(attempt_id);
