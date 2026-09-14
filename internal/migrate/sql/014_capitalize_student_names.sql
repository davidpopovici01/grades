-- 014_capitalize_student_names.sql
-- Ensure stored student names start with an uppercase letter.
-- UPPER() folds ASCII only, so non-ASCII characters (e.g. Chinese names) are unchanged.

UPDATE students
SET first_name = UPPER(SUBSTR(first_name, 1, 1)) || SUBSTR(first_name, 2)
WHERE SUBSTR(first_name, 1, 1) != UPPER(SUBSTR(first_name, 1, 1));

UPDATE students
SET last_name = UPPER(SUBSTR(last_name, 1, 1)) || SUBSTR(last_name, 2)
WHERE SUBSTR(last_name, 1, 1) != UPPER(SUBSTR(last_name, 1, 1));

UPDATE students
SET chinese_name = UPPER(SUBSTR(chinese_name, 1, 1)) || SUBSTR(chinese_name, 2)
WHERE chinese_name IS NOT NULL
  AND SUBSTR(chinese_name, 1, 1) != UPPER(SUBSTR(chinese_name, 1, 1));
