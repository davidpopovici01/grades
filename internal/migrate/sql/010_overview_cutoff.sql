-- 010_overview_cutoff.sql
-- Add overview cutoff assignment ID to course_year_terms

ALTER TABLE course_year_terms ADD COLUMN overview_cutoff_assignment_id INTEGER;
