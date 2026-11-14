-- Addresses that must not be sent to any more (DECISIONS 35).
--
-- When the provider says a phone number or a mailbox does not exist, trying
-- again in the next campaign cannot help and costs a request every time. The
-- recipient is suppressed on that channel: later campaigns do not queue a
-- message for them, and one already queued is skipped at send time.
--
-- One row per person and channel. The other channel is not affected: a dead
-- phone number says nothing about the email address.
CREATE TABLE suppressions (
    user_id     BIGINT      NOT NULL REFERENCES users (id),
    channel     TEXT        NOT NULL CHECK (channel IN ('sms', 'email')),
    -- Why. A closed set, so a new reason is a deliberate migration.
    reason      TEXT        NOT NULL CHECK (reason IN ('invalid_address')),
    -- The campaign whose message found it out, and what the provider said.
    campaign_id BIGINT      REFERENCES campaigns (id) ON DELETE SET NULL,
    detail      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, channel)
);
