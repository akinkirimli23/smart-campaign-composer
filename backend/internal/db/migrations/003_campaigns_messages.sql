-- An approved campaign and its outbound queue.

CREATE TABLE campaigns (
    id          BIGSERIAL PRIMARY KEY,
    brief       TEXT        NOT NULL,
    -- Snapshots of what the marketer approved. Both were validated by the
    -- application before they got here; they are never executed or parsed as SQL.
    rule        JSONB       NOT NULL,
    content     JSONB       NOT NULL,
    -- The instant the audience was evaluated at.
    as_of       TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per recipient and channel. The table is the queue (DECISIONS 2):
-- workers claim rows with SELECT ... FOR UPDATE SKIP LOCKED.
CREATE TABLE messages (
    id                  BIGSERIAL PRIMARY KEY,
    campaign_id         BIGINT      NOT NULL REFERENCES campaigns (id) ON DELETE CASCADE,
    user_id             BIGINT      NOT NULL REFERENCES users (id),
    channel             TEXT        NOT NULL CHECK (channel IN ('sms', 'email')),
    -- pending: waiting for its first or next attempt
    -- sending: claimed by a worker until locked_until
    -- sent / failed / skipped: final
    status              TEXT        NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'skipped')),
    attempts            INT         NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    -- Retry state lives in the row, so it survives a restart (DECISIONS 21).
    next_attempt_at     TIMESTAMPTZ NOT NULL,
    locked_until        TIMESTAMPTZ,
    last_error          TEXT,
    provider_message_id TEXT,
    sent_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The guarantee that a campaign reaches a person at most once per channel.
    CONSTRAINT messages_once_per_channel UNIQUE (campaign_id, user_id, channel),
    CONSTRAINT messages_sending_has_lease CHECK ((status = 'sending') = (locked_until IS NOT NULL))
);

-- Serves the claim query: unfinished rows of a channel, oldest due first.
CREATE INDEX messages_queue_idx ON messages (channel, next_attempt_at)
    WHERE status IN ('pending', 'sending');
