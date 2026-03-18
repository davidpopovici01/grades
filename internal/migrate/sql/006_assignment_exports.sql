CREATE TABLE IF NOT EXISTS assignment_exports (
  assignment_id INTEGER PRIMARY KEY,
  export_hash   TEXT NOT NULL,
  export_path   TEXT,
  exported_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  FOREIGN KEY (assignment_id) REFERENCES assignments(assignment_id) ON DELETE CASCADE
);
