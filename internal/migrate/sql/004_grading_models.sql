CREATE TABLE IF NOT EXISTS category_grading_policies (
  course_year_id INTEGER NOT NULL,
  term_id        INTEGER NOT NULL,
  category_id    INTEGER NOT NULL,
  scheme_key     TEXT NOT NULL DEFAULT 'average',
  PRIMARY KEY (course_year_id, term_id, category_id),
  FOREIGN KEY (course_year_id) REFERENCES course_years(course_year_id) ON DELETE CASCADE,
  FOREIGN KEY (term_id) REFERENCES terms(term_id) ON DELETE CASCADE,
  FOREIGN KEY (category_id) REFERENCES categories(category_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS assignment_curves (
  assignment_id   INTEGER PRIMARY KEY,
  anchor_percent  REAL NOT NULL DEFAULT 100 CHECK (anchor_percent > 0),
  lift_percent    REAL NOT NULL DEFAULT 0 CHECK (lift_percent >= 0 AND lift_percent <= 100),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  FOREIGN KEY (assignment_id) REFERENCES assignments(assignment_id) ON DELETE CASCADE
);

ALTER TABLE grades ADD COLUMN redo_count INTEGER NOT NULL DEFAULT 0 CHECK (redo_count >= 0);
