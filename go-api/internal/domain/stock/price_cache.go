package stock

type PriceCache interface {
	Push(code StockCode, price Price) error
	GetHistory(code StockCode, n int) ([]Price, error)
	LastDate(code StockCode) (string, error)
	SetLastDate(code StockCode, date string) error
}
