package stock

type PriceFetcher interface {
	FetchLatest(code StockCode) (Price, error)
}
