-- 011_password_changed_at.sql
-- Add password_changed_at timestamp to student_accounts for merge logic.

ALTER TABLE student_accounts ADD COLUMN password_changed_at TEXT;

-- Initialize existing rows with a default old timestamp.
UPDATE student_accounts SET password_changed_at = '1970-01-01T00:00:00Z' WHERE password_changed_at IS NULL;
