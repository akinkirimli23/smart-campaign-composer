-- A purchase always carries an amount; a cart_add never does.
-- The seed generator already guarantees this (TestGenerateInvariants); the
-- constraint makes the database enforce it for every future writer as well.
-- Category is deliberately not constrained here, see DECISIONS 12.

ALTER TABLE events
    ADD CONSTRAINT events_amount_iff_purchase
    CHECK ((event_type = 'purchase') = (amount IS NOT NULL));
