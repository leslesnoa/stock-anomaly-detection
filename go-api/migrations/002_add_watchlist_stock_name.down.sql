-- 001_initial_schema.down.sqlと同じ理由でIF EXISTSを使う。
ALTER TABLE IF EXISTS watchlist DROP COLUMN IF EXISTS stock_name;
