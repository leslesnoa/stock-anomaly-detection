package persistence

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
)

const uniqueViolationCode = "23505"

type PgUserRepository struct {
	conn *pgx.Conn
}

func NewPgUserRepository(conn *pgx.Conn) *PgUserRepository {
	return &PgUserRepository{conn: conn}
}

func (r *PgUserRepository) Create(ctx context.Context, u user.User) error {
	_, err := r.conn.Exec(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)`,
		u.Email, u.PasswordHash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return user.ErrEmailAlreadyExists
		}
		return err
	}
	return nil
}

func (r *PgUserRepository) FindByEmail(ctx context.Context, email string) (user.User, error) {
	var u user.User
	err := r.conn.QueryRow(ctx,
		`SELECT id, email, password_hash, slack_webhook_url, created_at FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.SlackWebhookURL, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, user.ErrNotFound
		}
		return user.User{}, err
	}
	return u, nil
}
