CREATE TABLE curu.session_activity_placements (
    id uuid PRIMARY KEY,
    block_id uuid NOT NULL REFERENCES curu.session_blocks(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    source_activity_id uuid NOT NULL,
    source_activity_revision bigint NOT NULL CHECK (source_activity_revision > 0),
    source_identity text,
    title text NOT NULL CHECK (octet_length(title) BETWEEN 1 AND 200),
    summary text NOT NULL DEFAULT '' CHECK (octet_length(summary) <= 1000),
    content_markdown text NOT NULL DEFAULT '' CHECK (octet_length(content_markdown) <= 100000),
    recommended_duration_seconds integer CHECK (recommended_duration_seconds IS NULL OR recommended_duration_seconds BETWEEN 0 AND 86400),
    phase text NOT NULL DEFAULT '' CHECK (octet_length(phase) <= 100),
    theme text NOT NULL DEFAULT '' CHECK (octet_length(theme) <= 100),
    tags text[] NOT NULL DEFAULT '{}'::text[] CHECK (cardinality(tags) <= 10),
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp,
    UNIQUE (block_id, position)
);

CREATE TABLE curu.session_placement_progressions (
    id uuid PRIMARY KEY,
    placement_id uuid NOT NULL REFERENCES curu.session_activity_placements(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    title text NOT NULL DEFAULT '' CHECK (octet_length(title) <= 200),
    content_markdown text NOT NULL DEFAULT '' CHECK (octet_length(content_markdown) <= 100000),
    recommended_duration_seconds integer CHECK (recommended_duration_seconds IS NULL OR recommended_duration_seconds BETWEEN 0 AND 86400),
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp,
    UNIQUE (placement_id, position)
);
