CREATE TABLE IF NOT EXISTS student_accounts (
  student_pk            INTEGER PRIMARY KEY,
  username              TEXT NOT NULL UNIQUE,
  password_salt         TEXT NOT NULL,
  password_hash         TEXT NOT NULL,
  must_change_password  INTEGER NOT NULL DEFAULT 1,
  password_changed_at   TEXT,
  created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  FOREIGN KEY (student_pk) REFERENCES students(student_pk) ON DELETE CASCADE
);
