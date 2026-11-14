CREATE TABLE curu.sessions (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    title text NOT NULL CHECK (octet_length(title) BETWEEN 1 AND 200),
    session_date date,
    group_context text NOT NULL DEFAULT '' CHECK (octet_length(group_context) <= 10000),
    intent text NOT NULL DEFAULT '' CHECK (octet_length(intent) <= 10000),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp
);

CREATE INDEX sessions_owner_current_idx ON curu.sessions (owner_id, session_date, title, id) WHERE archived_at IS NULL;
CREATE INDEX sessions_owner_archived_idx ON curu.sessions (owner_id, archived_at DESC, id) WHERE archived_at IS NOT NULL;

CREATE TABLE curu.session_blocks (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES curu.sessions(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 200),
    intent text NOT NULL DEFAULT '' CHECK (octet_length(intent) <= 10000),
    notes text NOT NULL DEFAULT '' CHECK (octet_length(notes) <= 10000),
    observation_lens text NOT NULL DEFAULT '' CHECK (octet_length(observation_lens) <= 10000),
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp,
    UNIQUE (session_id, position)
);
