package stock

import "errors"

type StockCode string

func NewStockCode(code string) (StockCode, error) {
	if code == "" {
		return "", errors.New("stock code cannot be empty")
	}
	return StockCode(code), nil
}

func (s StockCode) String() string { return string(s) }

type Price float64
type Volume int64
