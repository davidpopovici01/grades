ALTER TABLE assignments ADD COLUMN weight_percent REAL NOT NULL DEFAULT 0 CHECK (weight_percent >= 0 AND weight_percent <= 100);
