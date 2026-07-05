package stock

type PriceCache interface {
	Push(code StockCode, price Price) error
	GetHistory(code StockCode, n int) ([]Price, error)
}
