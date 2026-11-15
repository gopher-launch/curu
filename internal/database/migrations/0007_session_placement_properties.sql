ALTER TABLE curu.session_activity_placements
    ADD COLUMN local_notes text NOT NULL DEFAULT '' CHECK (octet_length(local_notes) <= 10000),
    ADD COLUMN duration_seconds integer CHECK (duration_seconds IS NULL OR duration_seconds BETWEEN 0 AND 86400);
