-- 日次終値の単一の正となるストア。チャート描画・統計的な予測帯の算出・
-- 異常検知の履歴ウィンドウのすべてがこのテーブルを参照する。
--
-- 主キーを (stock_code, date) の複合自然キーにしている理由:
--   1. 同一銘柄・同一取引日の重複投入を DB 制約で弾きたい（ON CONFLICT DO NOTHING による冪等なバックフィル）
--   2. 「銘柄を指定して日付降順に直近n件」がこの主キーインデックスだけで完結する
-- 他テーブルの UUID PK 規約に対する意図的な逸脱。
CREATE TABLE IF NOT EXISTS daily_prices (
    stock_code TEXT        NOT NULL,
    date       DATE        NOT NULL,
    close      NUMERIC     NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (stock_code, date)
);
