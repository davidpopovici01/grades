CREATE TABLE IF NOT EXISTS category_exports (
  course_year_id INTEGER NOT NULL,
  term_id        INTEGER NOT NULL,
  category_id    INTEGER NOT NULL,
  export_hash    TEXT NOT NULL,
  export_path    TEXT,
  export_rows_json TEXT,
  exported_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (course_year_id, term_id, category_id),
  FOREIGN KEY (course_year_id, term_id) REFERENCES course_year_terms(course_year_id, term_id) ON DELETE CASCADE,
  FOREIGN KEY (category_id) REFERENCES categories(category_id) ON DELETE CASCADE
);
