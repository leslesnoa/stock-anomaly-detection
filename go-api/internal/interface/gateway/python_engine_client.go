package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type PythonEngineClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewPythonEngineClient(baseURL string) *PythonEngineClient {
	return &PythonEngineClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type analyzeRequest struct {
	StockCode    string    `json:"stock_code"`
	ZScore       float64   `json:"z_score"`
	CurrentPrice float64   `json:"current_price"`
	Prices       []float64 `json:"prices"`
}

type analyzeResponse struct {
	StockCode  string `json:"stock_code"`
	Indicators struct {
		RSI  *float64 `json:"rsi"`
		MACD struct {
			Line      *float64 `json:"line"`
			Signal    *float64 `json:"signal"`
			Histogram *float64 `json:"histogram"`
		} `json:"macd"`
		Bollinger struct {
			Upper  *float64 `json:"upper"`
			Middle *float64 `json:"middle"`
			Lower  *float64 `json:"lower"`
		} `json:"bollinger"`
	} `json:"indicators"`
}

func (c *PythonEngineClient) Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (analysis.Indicators, error) {
	reqBody, err := json.Marshal(analyzeRequest{
		StockCode:    code.String(),
		ZScore:       zScore,
		CurrentPrice: currentPrice,
		Prices:       prices,
	})
	if err != nil {
		return analysis.Indicators{}, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/analyze", bytes.NewReader(reqBody))
	if err != nil {
		return analysis.Indicators{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return analysis.Indicators{}, fmt.Errorf("call python engine: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return analysis.Indicators{}, fmt.Errorf("python engine returned status %d", resp.StatusCode)
	}

	var result analyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return analysis.Indicators{}, fmt.Errorf("decode response: %w", err)
	}

	return analysis.Indicators{
		RSI: result.Indicators.RSI,
		MACD: analysis.MACD{
			Line:      result.Indicators.MACD.Line,
			Signal:    result.Indicators.MACD.Signal,
			Histogram: result.Indicators.MACD.Histogram,
		},
		Bollinger: analysis.Bollinger{
			Upper:  result.Indicators.Bollinger.Upper,
			Middle: result.Indicators.Bollinger.Middle,
			Lower:  result.Indicators.Bollinger.Lower,
		},
	}, nil
}
