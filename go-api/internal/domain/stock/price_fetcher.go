package stock

type PriceFetcher interface {
	FetchLatest(code StockCode) (Quote, error)
}
