package persistence

import (
	"context"
	"database/sql"
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

func (r *PgNotificationRepository) FindByStockCode(ctx context.Context, stockCode string) ([]notification.Notification, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT user_id, stock_code, anomaly_score, ai_report, technical_indicators, slack_sent, notified_at
		 FROM notifications
		 WHERE stock_code = $1
		 ORDER BY notified_at ASC`,
		stockCode)
	if err != nil {
		return nil, fmt.Errorf("find notifications %s: %w", stockCode, err)
	}
	defer rows.Close()

	notifications := []notification.Notification{}
	for rows.Next() {
		var n notification.Notification
		var userID sql.NullString
		var aiReport sql.NullString
		var indicatorsJSON []byte
		if err := rows.Scan(&userID, &n.StockCode, &n.AnomalyScore, &aiReport, &indicatorsJSON, &n.SlackSent, &n.NotifiedAt); err != nil {
			return nil, fmt.Errorf("scan notification %s: %w", stockCode, err)
		}
		if userID.Valid {
			n.UserID = &userID.String
		}
		n.AIReport = aiReport.String
		if len(indicatorsJSON) > 0 {
			if err := json.Unmarshal(indicatorsJSON, &n.TechnicalIndicators); err != nil {
				return nil, fmt.Errorf("unmarshal technical indicators %s: %w", stockCode, err)
			}
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find notifications %s: %w", stockCode, err)
	}
	return notifications, nil
}
