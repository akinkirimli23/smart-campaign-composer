-- What the provider accepted for this recipient, after personalisation
-- (DECISIONS 44). Written together with status 'sent'; NULL for a message that
-- was never delivered and for messages sent before this migration.
-- sent_subject is the email subject; an SMS has none.
ALTER TABLE messages
    ADD COLUMN sent_subject text,
    ADD COLUMN sent_body    text;
