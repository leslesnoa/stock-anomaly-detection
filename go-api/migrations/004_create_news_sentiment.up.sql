CREATE TABLE IF NOT EXISTS news_articles (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stock_code           TEXT NOT NULL,
    tdnet_id             TEXT NOT NULL,
    title                TEXT NOT NULL,
    url                  TEXT NOT NULL,
    published_at         TIMESTAMPTZ NOT NULL,
    sentiment            TEXT CHECK (sentiment IN ('bullish', 'bearish', 'neutral')),
    sentiment_confidence SMALLINT CHECK (sentiment_confidence BETWEEN 0 AND 100),
    scored_by            TEXT,
    scored_at            TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (stock_code, tdnet_id)
);
CREATE INDEX IF NOT EXISTS news_articles_stock_published_idx
    ON news_articles (stock_code, published_at DESC);

CREATE TABLE IF NOT EXISTS stock_sentiment_snapshots (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stock_code                TEXT NOT NULL,
    bullish_score             SMALLINT CHECK (bullish_score BETWEEN 0 AND 100),
    bearish_score             SMALLINT CHECK (bearish_score BETWEEN 0 AND 100),
    impact_score              SMALLINT CHECK (impact_score BETWEEN 0 AND 100),
    confidence_score          SMALLINT CHECK (confidence_score BETWEEN 0 AND 100),
    short_term_up_probability SMALLINT CHECK (short_term_up_probability BETWEEN 0 AND 100),
    article_count             INTEGER NOT NULL,
    input_fingerprint         TEXT NOT NULL,
    scored_by                 TEXT NOT NULL,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    checked_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS stock_sentiment_snapshots_stock_created_idx
    ON stock_sentiment_snapshots (stock_code, created_at DESC);
