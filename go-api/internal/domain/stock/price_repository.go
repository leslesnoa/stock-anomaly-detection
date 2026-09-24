package stock

import "context"

// PriceRepository は日次終値の永続化を担う。Quote.Date は "YYYY-MM-DD"（JST基準）。
//
// Quote 自身が日付を持つため、最終取引日を別途管理する必要がない。
// 「同一取引日を二重に積んで標準偏差を歪める」事故を DB の主キー制約で防げる。
// DB アクセスは呼び出し元のキャンセルに追従させたいので ctx を取る。
type PriceRepository interface {
	// Save は1件の終値を保存する。同一 (code, quote.Date) が既にあれば何もしない。
	Save(ctx context.Context, code StockCode, quote Quote) error
	// SaveAll は複数の終値をまとめて保存する。既存分は無視する（冪等）。
	SaveAll(ctx context.Context, code StockCode, quotes []Quote) error
	// FindRecent は直近n件の終値を日付の古い順で返す。該当なしなら空スライス。
	FindRecent(ctx context.Context, code StockCode, n int) ([]Quote, error)
	// LatestDate は保存済みの最新取引日を返す。1件も無ければ空文字を返す（エラーにしない）。
	LatestDate(ctx context.Context, code StockCode) (string, error)
}
