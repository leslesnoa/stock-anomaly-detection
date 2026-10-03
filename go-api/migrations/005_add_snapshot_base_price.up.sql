-- スコア算出時点の最新終値の日付と値。short_term_up_probability が当たったかを後から検証するために残す。
ALTER TABLE stock_sentiment_snapshots ADD COLUMN IF NOT EXISTS base_price_date DATE;
ALTER TABLE stock_sentiment_snapshots ADD COLUMN IF NOT EXISTS base_close NUMERIC;
