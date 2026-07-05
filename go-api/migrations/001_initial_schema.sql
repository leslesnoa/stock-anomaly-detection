CREATE TABLE IF NOT EXISTS users (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email             TEXT UNIQUE NOT NULL,
    password_hash     TEXT NOT NULL,
    slack_webhook_url TEXT,
    created_at        TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS watchlist (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID REFERENCES users(id) ON DELETE CASCADE,
    stock_code      TEXT NOT NULL,
    alert_threshold NUMERIC DEFAULT 2.5,
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(user_id, stock_code)
);

CREATE TABLE IF NOT EXISTS notifications (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID REFERENCES users(id),
    stock_code           TEXT NOT NULL,
    anomaly_score        NUMERIC NOT NULL,
    ai_report            TEXT,
    technical_indicators JSONB,
    slack_sent           BOOLEAN DEFAULT false,
    notified_at          TIMESTAMPTZ DEFAULT now()
);
