-- 016_rubrics.sql
-- DOCX rubrics linked to assignments, plus per-student rubric scores

CREATE TABLE IF NOT EXISTS assignment_rubrics (
  assignment_id  INTEGER PRIMARY KEY,
  title          TEXT NOT NULL,
  criteria_json  TEXT NOT NULL,          -- [{"name":"Code Organization","max_points":5}, ...]
  template_docx  BLOB NOT NULL,          -- original .docx bytes (robust to the source file moving)
  template_name  TEXT NOT NULL,
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  FOREIGN KEY (assignment_id) REFERENCES assignments(assignment_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS rubric_scores (
  assignment_id INTEGER NOT NULL,
  student_pk    INTEGER NOT NULL,
  scores_json   TEXT NOT NULL,           -- [4,5,3,...]
  total         REAL NOT NULL,
  file_path     TEXT,
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (assignment_id, student_pk),
  FOREIGN KEY (assignment_id) REFERENCES assignments(assignment_id) ON DELETE CASCADE,
  FOREIGN KEY (student_pk) REFERENCES students(student_pk) ON DELETE CASCADE
);
