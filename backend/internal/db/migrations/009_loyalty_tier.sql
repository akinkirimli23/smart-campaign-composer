-- Loyalty tier: a closed-set attribute of the user, like city or gender
-- (DECISIONS 45). The seed derives it from what the user has spent; a rule
-- only ever reads the column.
ALTER TABLE users
    ADD COLUMN loyalty_tier TEXT NOT NULL DEFAULT 'bronze'
        CHECK (loyalty_tier IN ('bronze', 'silver', 'gold'));

-- Databases seeded before this migration get the tier the seed would have
-- given: the thresholds are seed.SilverSpend and seed.GoldSpend. On a fresh
-- database there are no users yet and this does nothing.
UPDATE users u
SET loyalty_tier = CASE WHEN t.total >= 8000 THEN 'gold' ELSE 'silver' END
FROM (SELECT user_id, sum(amount) AS total FROM events WHERE event_type = 'purchase' GROUP BY user_id) t
WHERE t.user_id = u.id AND t.total >= 3000;
