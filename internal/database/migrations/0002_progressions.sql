CREATE TABLE curu.activity_progressions (
    id uuid PRIMARY KEY,
    activity_id uuid NOT NULL REFERENCES curu.activities(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    title text NOT NULL DEFAULT '' CHECK (length(title) <= 200),
    content_markdown text NOT NULL DEFAULT '',
    duration_seconds integer CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp,
    UNIQUE (activity_id, position)
);
