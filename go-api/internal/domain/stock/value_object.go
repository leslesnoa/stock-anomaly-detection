package stock

import (
	"errors"
	"regexp"
)

type StockCode string

var stockCodeRegexp = regexp.MustCompile(`^\d{4}$`)

func NewStockCode(code string) (StockCode, error) {
	if !stockCodeRegexp.MatchString(code) {
		return "", errors.New("stock code must be 4 digits")
	}
	return StockCode(code), nil
}

func (s StockCode) String() string { return string(s) }

type Price float64
type Volume int64
