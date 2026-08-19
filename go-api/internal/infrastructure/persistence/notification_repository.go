package persistence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
)

type PgNotificationRepository struct {
	conn *pgxpool.Pool
}

func NewPgNotificationRepository(conn *pgxpool.Pool) *PgNotificationRepository {
	return &PgNotificationRepository{conn: conn}
}

func (r *PgNotificationRepository) Save(ctx context.Context, n notification.Notification) error {
	indicatorsJSON, err := json.Marshal(n.TechnicalIndicators)
	if err != nil {
		return fmt.Errorf("marshal technical indicators: %w", err)
	}

	_, err = r.conn.Exec(ctx,
		`INSERT INTO notifications (user_id, stock_code, anomaly_score, ai_report, technical_indicators, slack_sent)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		n.UserID, n.StockCode, n.AnomalyScore, n.AIReport, indicatorsJSON, n.SlackSent)
	return err
}
