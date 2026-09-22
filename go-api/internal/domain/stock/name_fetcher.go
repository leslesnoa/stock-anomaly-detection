package stock

type NameFetcher interface {
	FetchCompanyName(code StockCode) (string, error)
}
