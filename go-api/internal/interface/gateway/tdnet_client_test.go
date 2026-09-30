package gateway_test

import (
	"context"
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

func TestYanoshinTDnetClient_FetchDisclosures(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "100", r.URL.Query().Get("limit"))
		resp := map[string]any{
			"items": []map[string]any{
				{"Tdnet": map[string]string{
					"id": "1279204", "title": "自己株式の取得状況に関するお知らせ",
					"pubdate":      "2026-09-03 15:30:00",
					"document_url": "https://webapi.yanoshin.jp/rd.php?https://www.release.tdnet.info/inbs/a.pdf",
				}},
				{"Tdnet": map[string]string{
					"id": "1270000", "title": "範囲外の古い開示",
					"pubdate": "2026-06-01 15:00:00", "document_url": "https://example.com/old.pdf",
				}},
				{"Tdnet": map[string]string{
					"id": "1279999", "title": "日付が壊れている開示",
					"pubdate": "not-a-date", "document_url": "https://example.com/broken.pdf",
				}},
				{"Tdnet": map[string]string{
					"id": "", "title": "IDが無い開示",
					"pubdate": "2026-09-04 15:30:00", "document_url": "https://example.com/noid.pdf",
				}},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")
	since := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

	got, err := client.FetchDisclosures(context.Background(), code, since)
	require.NoError(t, err)
	require.Len(t, got, 1, "範囲外・日付不正・ID無しは除外される")
	assert.Equal(t, "1279204", got[0].TdnetID)
	assert.Equal(t, "自己株式の取得状況に関するお知らせ", got[0].Title)
	assert.Equal(t, "https://webapi.yanoshin.jp/rd.php?https://www.release.tdnet.info/inbs/a.pdf", got[0].URL)
	assert.True(t, got[0].PublishedAt.Equal(time.Date(2026, 9, 3, 6, 30, 0, 0, time.UTC)), "pubdateはJSTとして解釈する")
}

func TestYanoshinTDnetClient_FetchDisclosures_EmptyReturnsEmptySlice(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"items": []any{}}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchDisclosures(context.Background(), code, time.Now().AddDate(0, 0, -90))
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestYanoshinTDnetClient_FetchDisclosures_NonOKStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.FetchDisclosures(context.Background(), code, time.Now())
	require.Error(t, err)
}
