CREATE TABLE IF NOT EXISTS category_aliases (
  alias_id     INTEGER PRIMARY KEY AUTOINCREMENT,
  alias_name   TEXT NOT NULL UNIQUE,
  category_id  INTEGER NOT NULL,
  FOREIGN KEY (category_id) REFERENCES categories(category_id) ON DELETE CASCADE
);
