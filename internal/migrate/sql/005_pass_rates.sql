ALTER TABLE category_grading_policies ADD COLUMN default_pass_percent REAL;

ALTER TABLE assignments ADD COLUMN pass_percent REAL;
