-- 001_initial_schema.down.sqlと同じ理由でIF EXISTSを使う。
ALTER TABLE IF EXISTS stock_sentiment_snapshots DROP COLUMN IF EXISTS base_close;
ALTER TABLE IF EXISTS stock_sentiment_snapshots DROP COLUMN IF EXISTS base_price_date;
