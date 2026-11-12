-- Scheduled sending and cancellation (DECISIONS 27).

-- When delivery may start. Existing campaigns were all immediate.
ALTER TABLE campaigns ADD COLUMN send_at TIMESTAMPTZ;
UPDATE campaigns SET send_at = as_of;
ALTER TABLE campaigns ALTER COLUMN send_at SET NOT NULL;

-- Set once, when the marketer cancels. Messages not sent by then never are.
ALTER TABLE campaigns ADD COLUMN cancelled_at TIMESTAMPTZ;

-- A cancelled message is final, like sent / failed / skipped.
ALTER TABLE messages DROP CONSTRAINT messages_status_check;
ALTER TABLE messages ADD CONSTRAINT messages_status_check
    CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'skipped', 'cancelled'));
