ALTER TABLE curu.activities
    ADD COLUMN phase text NOT NULL DEFAULT '' CHECK (length(phase) <= 100),
    ADD COLUMN theme text NOT NULL DEFAULT '' CHECK (length(theme) <= 100),
    ADD COLUMN tags text[] NOT NULL DEFAULT '{}'::text[] CHECK (cardinality(tags) <= 10);
