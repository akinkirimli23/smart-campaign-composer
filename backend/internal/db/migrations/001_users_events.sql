-- Recipients (fake customers) and their behavioural events.
-- Segment rules produced by the LLM are compiled against these two tables only.

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    first_name    TEXT        NOT NULL,
    last_name     TEXT        NOT NULL,
    email         TEXT        UNIQUE,          -- NULL: user has no email address
    phone         TEXT        UNIQUE,          -- NULL: user has no phone number (E.164)
    city          TEXT        NOT NULL,
    language      TEXT        NOT NULL CHECK (language IN ('tr', 'en')),
    gender        TEXT        NOT NULL CHECK (gender IN ('female', 'male')),
    birth_year    INT         NOT NULL CHECK (birth_year BETWEEN 1920 AND 2015),
    signup_at     TIMESTAMPTZ NOT NULL,
    -- Channel consent. A message is only ever sent on a channel the user opted in to
    -- AND for which the user has an address.
    email_opt_in  BOOLEAN     NOT NULL DEFAULT FALSE,
    sms_opt_in    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX users_city_idx ON users (city);

CREATE TABLE events (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    event_type   TEXT        NOT NULL CHECK (event_type IN ('cart_add', 'purchase')),
    category     TEXT        NOT NULL,
    amount       NUMERIC(10, 2),               -- set for purchases only
    occurred_at  TIMESTAMPTZ NOT NULL
);

-- Serves the EXISTS / NOT EXISTS sub-queries the segment compiler emits:
-- "has a <type> event (in <category>) since <time>".
CREATE INDEX events_user_type_time_idx ON events (user_id, event_type, occurred_at);
