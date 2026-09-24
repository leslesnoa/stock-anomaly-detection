package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestYahooFinanceClient_FetchHistory_ExcludesTodayUnsettledClose(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	now := time.Now().In(jst)
	today := time.Date(now.Year(), now.Month(), now.Day(), 15, 0, 0, 0, jst)
	yesterday := today.AddDate(0, 0, -1)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"timestamp": []int64{yesterday.Unix(), today.Unix()},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, 3250.0}}, // 当日分はnullではないが、取引中の速報値
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
	require.Len(t, quotes, 1)
	assert.Equal(t, stock.Price(3200.0), quotes[0].Price)
	assert.Equal(t, yesterday.Format("2006-01-02"), quotes[0].Date)
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

func TestYahooFinanceClient_FetchCompanyName_PrefersLongName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1d", r.URL.Query().Get("range"))
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"meta": map[string]any{
							"longName":  "Toyota Motor Corporation",
							"shortName": "TOYOTA MOTOR CORP",
						},
						"timestamp": []int64{1783404000},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3250.0}},
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

	name, err := client.FetchCompanyName(code)
	require.NoError(t, err)
	assert.Equal(t, "Toyota Motor Corporation", name)
}

func TestYahooFinanceClient_FetchCompanyName_FallsBackToShortName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"meta": map[string]any{
							"shortName": "TOYOTA MOTOR CORP",
						},
						"timestamp": []int64{1783404000},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3250.0}},
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

	name, err := client.FetchCompanyName(code)
	require.NoError(t, err)
	assert.Equal(t, "TOYOTA MOTOR CORP", name)
}

func TestYahooFinanceClient_FetchCompanyName_EmptyWhenNoNameInMeta(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"meta":      map[string]any{},
						"timestamp": []int64{1783404000},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3250.0}},
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

	name, err := client.FetchCompanyName(code)
	require.NoError(t, err)
	assert.Equal(t, "", name)
}

func TestYahooFinanceClient_FetchCompanyName_ErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/0000.T", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchCompanyName(code)
	require.Error(t, err)
}

func TestYahooFinanceClient_FetchHistory_SelectsRangeFromRequestedDays(t *testing.T) {
	tests := []struct {
		name          string
		days          int
		expectedRange string
	}{
		{name: "30営業日は3mo", days: 30, expectedRange: "3mo"},
		{name: "境界の60営業日は3mo", days: 60, expectedRange: "3mo"},
		{name: "61営業日は1y", days: 61, expectedRange: "1y"},
		{name: "境界の250営業日は1y", days: 250, expectedRange: "1y"},
		{name: "251営業日は2y", days: 251, expectedRange: "2y"},
		{name: "500営業日は2y", days: 500, expectedRange: "2y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.expectedRange, r.URL.Query().Get("range"))
				assert.Equal(t, "1d", r.URL.Query().Get("interval"))

				resp := map[string]any{
					"chart": map[string]any{
						"result": []map[string]any{
							{
								"timestamp": []int64{1783317600},
								"indicators": map[string]any{
									"quote": []map[string]any{
										{"close": []any{3200.0}},
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

			_, err := client.FetchHistory(code, tt.days)
			require.NoError(t, err)
		})
	}
}
