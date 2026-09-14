package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYahooFinanceClient_FetchLatest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Mozilla/5.0", r.Header.Get("User-Agent"))
		assert.Equal(t, "5d", r.URL.Query().Get("range"))
		assert.Equal(t, "1d", r.URL.Query().Get("interval"))

		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// 2026-07-06, 2026-07-07 (JST 15:00)
						"timestamp": []int64{1783317600, 1783404000},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, 3250.0}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	quote, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), quote.Price)
	assert.Equal(t, "2026-07-07", quote.Date)
}

func TestYahooFinanceClient_FetchLatest_SkipsTrailingNullClose(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// 2026-07-06, 2026-07-07, 2026-07-08（当日分は未確定でnull）
						"timestamp": []int64{1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, 3250.0, nil}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	quote, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), quote.Price)
	assert.Equal(t, "2026-07-07", quote.Date)
}

func TestYahooFinanceClient_FetchLatest_NoData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/0000.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}

func TestYahooFinanceClient_FetchLatest_AllCloseValuesNull(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"timestamp": []int64{1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{nil, nil, nil}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}

func TestYahooFinanceClient_FetchLatest_TimestampShorterThanClose(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// timestamp is shorter than close, which would otherwise
						// cause an index-out-of-range panic when scanning closes.
						// The only close value within the truncated range is null,
						// so this should surface as "no quote data", not a panic.
						"timestamp": []int64{1783317600},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{nil, 3250.0, 3300.0}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}

func TestYahooFinanceClient_FetchLatest_ErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/0000.T", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}

func TestYahooFinanceClient_FetchHistory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Mozilla/5.0", r.Header.Get("User-Agent"))
		assert.Equal(t, "3mo", r.URL.Query().Get("range"))
		assert.Equal(t, "1d", r.URL.Query().Get("interval"))

		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// 2026-07-06, 2026-07-07, 2026-07-08 (JST 15:00)
						"timestamp": []int64{1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, 3250.0, 3300.0}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	quotes, err := client.FetchHistory(code, 30)
	require.NoError(t, err)
	require.Len(t, quotes, 3)
	assert.Equal(t, stock.Quote{Price: 3200.0, Date: "2026-07-06"}, quotes[0])
	assert.Equal(t, stock.Quote{Price: 3250.0, Date: "2026-07-07"}, quotes[1])
	assert.Equal(t, stock.Quote{Price: 3300.0, Date: "2026-07-08"}, quotes[2])
}

func TestYahooFinanceClient_FetchHistory_TrimsToRequestedDays(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// 5トレーディングデイ分あるが、直近2件だけを要求する
						"timestamp": []int64{1783058400, 1783144800, 1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3100.0, 3150.0, 3200.0, 3250.0, 3300.0}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	quotes, err := client.FetchHistory(code, 2)
	require.NoError(t, err)
	require.Len(t, quotes, 2)
	assert.Equal(t, stock.Quote{Price: 3250.0, Date: "2026-07-07"}, quotes[0])
	assert.Equal(t, stock.Quote{Price: 3300.0, Date: "2026-07-08"}, quotes[1])
}

func TestYahooFinanceClient_FetchHistory_SkipsNullCloses(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"timestamp": []int64{1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, nil, 3300.0}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	quotes, err := client.FetchHistory(code, 30)
	require.NoError(t, err)
	require.Len(t, quotes, 2)
	assert.Equal(t, stock.Quote{Price: 3200.0, Date: "2026-07-06"}, quotes[0])
	assert.Equal(t, stock.Quote{Price: 3300.0, Date: "2026-07-08"}, quotes[1])
}

func TestYahooFinanceClient_FetchHistory_NoData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/0000.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchHistory(code, 30)
	require.Error(t, err)
}
