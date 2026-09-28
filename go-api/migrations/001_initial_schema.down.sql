-- docker-composeのdocker-entrypoint-initdb.dは*.sqlをファイル名の昇順(asciibetical)に実行するため、
-- 同じ番号内では "down" < "up" によりこのファイルが001_initial_schema.up.sqlより先に実行される。
-- 初回起動時にまだテーブルが存在しない状態でDROPが走っても失敗しないよう、必ずIF EXISTSを使う。
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS watchlist;
DROP TABLE IF EXISTS users;
