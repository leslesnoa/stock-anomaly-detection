package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// dateLayout は Quote.Date の文字列表現。Yahoo Finance クライアントが
// JST で組み立てるフォーマットと一致させる。
const dateLayout = "2006-01-02"

type PgPriceRepository struct {
	conn *pgxpool.Pool
}

// コンパイル時にインターフェース適合を保証する。
var _ stock.PriceRepository = (*PgPriceRepository)(nil)

func NewPgPriceRepository(conn *pgxpool.Pool) *PgPriceRepository {
	return &PgPriceRepository{conn: conn}
}

// insertPriceSQL は同一 (stock_code, date) の再投入を黙って捨てる。
// バックフィルと日次ポーリングが同じ取引日を取りに行っても行が重複しないようにするため。
const insertPriceSQL = `INSERT INTO daily_prices (stock_code, date, close)
VALUES ($1, $2, $3)
ON CONFLICT (stock_code, date) DO NOTHING`

func (r *PgPriceRepository) Save(ctx context.Context, code stock.StockCode, quote stock.Quote) error {
	d, err := time.Parse(dateLayout, quote.Date)
	if err != nil {
		return fmt.Errorf("parse date %q: %w", quote.Date, err)
	}
	_, err = r.conn.Exec(ctx, insertPriceSQL, code.String(), d, float64(quote.Price))
	if err != nil {
		return fmt.Errorf("save price %s: %w", code, err)
	}
	return nil
}

// SaveAll は pgx.Batch で1往復にまとめて投入する。2年分（約500件）を1件ずつ
// Exec すると往復回数がそのままレイテンシになるため。
func (r *PgPriceRepository) SaveAll(ctx context.Context, code stock.StockCode, quotes []stock.Quote) error {
	if len(quotes) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, q := range quotes {
		d, err := time.Parse(dateLayout, q.Date)
		if err != nil {
			return fmt.Errorf("parse date %q: %w", q.Date, err)
		}
		batch.Queue(insertPriceSQL, code.String(), d, float64(q.Price))
	}

	br := r.conn.SendBatch(ctx, batch)
	for range quotes {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return fmt.Errorf("save prices %s: %w", code, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("save prices %s: %w", code, err)
	}
	return nil
}

// FindRecent は日付降順でn件取ってから昇順に並べ直して返す。
// 呼び出し元（Z-score計算・チャート描画）はいずれも古い順の配列を前提にしている。
func (r *PgPriceRepository) FindRecent(ctx context.Context, code stock.StockCode, n int) ([]stock.Quote, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT date, close FROM (
		     SELECT date, close FROM daily_prices
		     WHERE stock_code = $1
		     ORDER BY date DESC
		     LIMIT $2
		 ) AS recent
		 ORDER BY date ASC`,
		code.String(), n)
	if err != nil {
		return nil, fmt.Errorf("find recent prices %s: %w", code, err)
	}
	defer rows.Close()

	quotes := []stock.Quote{}
	for rows.Next() {
		var d time.Time
		var closePrice float64
		if err := rows.Scan(&d, &closePrice); err != nil {
			return nil, fmt.Errorf("scan price %s: %w", code, err)
		}
		quotes = append(quotes, stock.Quote{Price: stock.Price(closePrice), Date: d.Format(dateLayout)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find recent prices %s: %w", code, err)
	}
	return quotes, nil
}

// LatestDate は1件も無い場合を「まだ取り込んでいない」という正常状態として扱い、
// 空文字を返す。呼び出し元は空文字と実際の取引日を単純比較するだけで済む。
func (r *PgPriceRepository) LatestDate(ctx context.Context, code stock.StockCode) (string, error) {
	var d time.Time
	err := r.conn.QueryRow(ctx,
		`SELECT date FROM daily_prices WHERE stock_code = $1 ORDER BY date DESC LIMIT 1`,
		code.String()).Scan(&d)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("latest date %s: %w", code, err)
	}
	return d.Format(dateLayout), nil
}
