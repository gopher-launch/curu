ALTER TABLE curu.sessions
    ADD COLUMN allocation_seconds integer CHECK (allocation_seconds IS NULL OR allocation_seconds BETWEEN 0 AND 86400);

ALTER TABLE curu.session_blocks
    ADD COLUMN allocation_seconds integer CHECK (allocation_seconds IS NULL OR allocation_seconds BETWEEN 0 AND 86400);
