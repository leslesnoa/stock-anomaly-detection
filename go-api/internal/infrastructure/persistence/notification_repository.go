package persistence

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
)

type PgNotificationRepository struct {
	conn *pgx.Conn
}

func NewPgNotificationRepository(conn *pgx.Conn) *PgNotificationRepository {
	return &PgNotificationRepository{conn: conn}
}

func (r *PgNotificationRepository) Save(ctx context.Context, n notification.Notification) error {
	_, err := r.conn.Exec(ctx,
		`INSERT INTO notifications (user_id, stock_code, anomaly_score, ai_report, technical_indicators, slack_sent)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		n.UserID, n.StockCode, n.AnomalyScore, n.AIReport, n.TechnicalIndicators, n.SlackSent)
	return err
}
