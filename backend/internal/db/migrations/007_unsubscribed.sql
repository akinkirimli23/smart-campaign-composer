-- A second reason to stop sending to someone on a channel: they asked us to
-- (DECISIONS 37). The address works; the person no longer wants the messages.
ALTER TABLE suppressions DROP CONSTRAINT suppressions_reason_check;
ALTER TABLE suppressions ADD CONSTRAINT suppressions_reason_check
    CHECK (reason IN ('invalid_address', 'unsubscribed'));
