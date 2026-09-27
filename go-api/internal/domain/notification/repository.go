package notification

import "context"

type Repository interface {
	Save(ctx context.Context, n Notification) error
	// FindByStockCode はstockCodeに紐づく通知履歴を notified_at 昇順で返す。
	// 該当が無ければ空スライスを返す（エラーにしない）。
	FindByStockCode(ctx context.Context, stockCode string) ([]Notification, error)
}
