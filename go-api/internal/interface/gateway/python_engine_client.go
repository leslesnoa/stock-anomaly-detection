package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"
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

var _ forecast.Forecaster = (*PythonEngineClient)(nil)

type forecastRequest struct {
	StockCode    string    `json:"stock_code"`
	CurrentPrice float64   `json:"current_price"`
	Prices       []float64 `json:"prices"`
}

type forecastPointDTO struct {
	Step    int     `json:"step"`
	Center  float64 `json:"center"`
	Upper68 float64 `json:"upper_68"`
	Lower68 float64 `json:"lower_68"`
	Upper95 float64 `json:"upper_95"`
	Lower95 float64 `json:"lower_95"`
}

type forecastResponseDTO struct {
	StockCode string             `json:"stock_code"`
	Horizon   int                `json:"horizon"`
	Points    []forecastPointDTO `json:"points"`
}

func (c *PythonEngineClient) Forecast(ctx context.Context, code stock.StockCode, currentPrice float64, prices []float64) (forecast.Forecast, error) {
	reqBody, err := json.Marshal(forecastRequest{
		StockCode:    code.String(),
		CurrentPrice: currentPrice,
		Prices:       prices,
	})
	if err != nil {
		return forecast.Forecast{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/forecast", bytes.NewReader(reqBody))
	if err != nil {
		return forecast.Forecast{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return forecast.Forecast{}, fmt.Errorf("call python engine: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return forecast.Forecast{}, fmt.Errorf("python engine returned status %d", resp.StatusCode)
	}

	var result forecastResponseDTO
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return forecast.Forecast{}, fmt.Errorf("decode response: %w", err)
	}

	points := make([]forecast.Point, len(result.Points))
	for i, p := range result.Points {
		points[i] = forecast.Point{
			Step: p.Step, Center: p.Center,
			Upper68: p.Upper68, Lower68: p.Lower68,
			Upper95: p.Upper95, Lower95: p.Lower95,
		}
	}
	return forecast.Forecast{Horizon: result.Horizon, Points: points}, nil
}
