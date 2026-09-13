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

func TestYanoshinTDnetClient_FetchRecent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "5", r.URL.Query().Get("limit"))

		resp := map[string]any{
			"total_count": 2,
			"items": []map[string]any{
				{"Tdnet": map[string]string{
					"title":        "自己株式の取得状況に関するお知らせ",
					"company_code": "72030",
					"pubdate":      time.Now().Format("2006-01-02 15:04:05"),
				}},
				{"Tdnet": map[string]string{
					"title":        "業績予想の修正に関するお知らせ",
					"company_code": "72030",
					"pubdate":      time.Now().AddDate(0, 0, -1).Format("2006-01-02 15:04:05"),
				}},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "自己株式の取得状況に関するお知らせ", got[0].Headline)
	assert.Equal(t, "自己株式の取得状況に関するお知らせ", got[0].Summary)
	assert.Equal(t, "業績予想の修正に関するお知らせ", got[1].Headline)
}

func TestYanoshinTDnetClient_FetchRecent_LimitsToTop5(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		items := make([]map[string]any, 10)
		for i := range items {
			items[i] = map[string]any{"Tdnet": map[string]string{"title": "h", "company_code": "72030"}}
		}
		resp := map[string]any{"total_count": 10, "items": items}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	assert.Len(t, got, 5)
}

func TestYanoshinTDnetClient_FetchRecent_FiltersOldItems(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		recentPubdate := time.Now().Format("2006-01-02 15:04:05")
		oldPubdate := time.Now().AddDate(0, 0, -100).Format("2006-01-02 15:04:05")

		resp := map[string]any{
			"total_count": 2,
			"items": []map[string]any{
				{"Tdnet": map[string]string{
					"title":        "直近のお知らせ",
					"company_code": "72030",
					"pubdate":      recentPubdate,
				}},
				{"Tdnet": map[string]string{
					"title":        "古いお知らせ",
					"company_code": "72030",
					"pubdate":      oldPubdate,
				}},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "直近のお知らせ", got[0].Headline)
}

func TestYanoshinTDnetClient_FetchRecent_UnparseablePubdateIsKept(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"total_count": 1,
			"items": []map[string]any{
				{"Tdnet": map[string]string{
					"title":        "日付形式不明のお知らせ",
					"company_code": "72030",
					"pubdate":      "not-a-date",
				}},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "日付形式不明のお知らせ", got[0].Headline)
}

func TestYanoshinTDnetClient_FetchRecent_ErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.FetchRecent(code)
	require.Error(t, err)
}
