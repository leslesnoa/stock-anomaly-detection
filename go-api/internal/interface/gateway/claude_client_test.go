package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/require"
)

func TestClaudeClient_GenerateReport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model": "claude-opus-5",
			"content": []map[string]string{
				{"type": "text", "text": "出来高急増を伴う上昇であり、決算好感の可能性が高い。"},
			},
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
			"usage":         map[string]int{"input_tokens": 100, "output_tokens": 30},
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewClaudeClientWithBaseURL("test-api-key", srv.URL, "claude-opus-5")

	got, err := client.GenerateReport("銘柄コード：7203\n異常スコア：3.2σ\n分析してください。")
	require.NoError(t, err)
	require.Equal(t, "出来高急増を伴う上昇であり、決算好感の可能性が高い。", got)
}

func TestClaudeClient_GenerateReport_RetriesOnceThenFails(t *testing.T) {
	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewClaudeClientWithBaseURL("test-api-key", srv.URL, "claude-opus-5")

	_, err := client.GenerateReport("prompt")
	require.Error(t, err)
	require.Equal(t, 2, callCount, "1回失敗後にもう1回リトライするはず")
}
