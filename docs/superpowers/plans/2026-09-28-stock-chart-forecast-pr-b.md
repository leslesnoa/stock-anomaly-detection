# 株価チャート＋予測線（PR-B: 機能実装）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** python-engineに `POST /forecast`（ドリフト＋ボラティリティ帯の算出）を新設し、go-apiに `GET /stocks/{code}/chart`（価格・アラート境界帯・予測・通知履歴・現在zスコアを合成して返すAPI）を追加し、frontendに `/stocks/[code]` 詳細ページ（Rechartsでの可視化）を実装する。目的は売買シグナルではなく「今の株価が統計的にどのくらい異常か」「統計的にどう動くか」を可視化すること。

**Architecture:** 3層を下から積み上げる。(1) python-engineが対数リターンの平均・標準偏差からドリフト＋ボラ帯を計算する純粋関数として `/forecast` を実装。(2) go-apiがこの新設エンドポイントをgatewayクライアント経由で呼び、`daily_prices`（PR-Aで導入済み）とアラート境界帯（既存の異常検知と同じ窓の取り方を流用、`anomaly/service.go`自体は変更しない）・通知履歴（`notifications`テーブルへの新規クエリ追加）を合成する `GetStockChartUsecase` を新設。(3) frontendがそのJSONをRechartsで描画する `/stocks/[code]` ページを新設し、watchlist一覧からのリンクを追加する。

**Tech Stack:** Python(FastAPI/pydantic、既存の `/analyze` と同じ構成)、Go(標準`net/http` ServeMux、pgx/v5、testify/mock)、Next.js(App Router、Server Component + Recharts、Vitest)

**Spec:** `docs/superpowers/specs/2026-09-24-stock-chart-forecast-design.md`

## Global Constraints

- 予測は点予測ではなく「方向＋レンジ帯」。中心線＝直近120営業日の対数リターン平均の延長、幅＝日次リターン標準偏差×√h、±1σ(68%)と±1.96σ(95%)の二重帯、ホライズン20営業日。
- 過去区間の「アラート境界帯」（ローリング30日平均±`ANOMALY_THRESHOLD`σ）と未来区間の「統計的期待レンジ」（ドリフト＋ボラ帯）は別物として並記し、既存の `internal/domain/anomaly/service.go` は一切変更しない。
- チャートAPIの認可は「リクエストしたユーザーのwatchlistに無ければ404」。データの存在範囲と認可範囲を一致させる。
- Python engine障害時（呼び出し失敗・データ不足いずれも）は `forecast: null` で200を返し、実績チャートとアラート境界帯は通常どおり描画する。
- 異常検知の窓（30件）・既存の `historySize`/`backfillDays` 定数・`daily_prices`のスキーマは変更しない。
- このリポジトリの `frontend/` はNext.js 16（破壊的変更あり）。ダイナミックルートの `params` はPromiseなので `await params` で受け取る（`frontend/node_modules/next/dist/docs/01-app/03-api-reference/03-file-conventions/dynamic-routes.md` で確認済み）。

## Review Focus

- 銘柄がまだバックフィル中で `daily_prices` の件数が少ない（30件未満・121件未満）状態でチャートを開くと、`current_z_score`/`forecast`が正しく`null`になり200が返ること（画面を壊さない）。Task 5でテスト。
- 自分のwatchlistに無い銘柄コードをパスに指定すると404になること（他ユーザーの監視銘柄のデータを覗けないこと）。Task 5・Task 6でテスト。
- 証券コードとして不正な形式（`"12"`など）をパスに渡すと400になること。Task 6でテスト。
- Python engineが呼び出しエラー（タイムアウト・5xx）を返しても、チャートAPI全体は200を返し`forecast`だけが`null`になること（実績チャートは死なない）。Task 5でテスト。
- 一度も異常検知が発火していない銘柄（`notifications`が0件）でもエラーにならず空配列で返ること。Task 4・Task 5でテスト。

---

### Task 1: Python — forecast計算サービスとスキーマ

**Files:**
- Modify: `python-engine/app/schemas.py`
- Create: `python-engine/app/services/forecast.py`
- Test: `python-engine/tests/test_schemas.py`（追記）
- Test: `python-engine/tests/test_forecast_service.py`

**Interfaces:**
- Consumes: なし（このタスクが基盤）
- Produces: `app.schemas.ForecastRequest`（`stock_code: str`, `current_price: float`, `prices: list[float]`、`ESTIMATION_WINDOW + 1`件未満で`ValidationError`）、`app.schemas.ForecastPoint`（`step, center, upper_68, lower_68, upper_95, lower_95`）、`app.schemas.ForecastResponse`（`stock_code, horizon, points`）、`app.schemas.ESTIMATION_WINDOW`（120）、`app.schemas.FORECAST_HORIZON`（20）、`app.services.forecast.calculate_forecast(stock_code, current_price, prices) -> ForecastResponse`。Task 2がこれらを使う。

- [ ] **Step 1: 失敗するテストを書く（スキーマのバリデーション）**

`python-engine/tests/test_schemas.py` の末尾に追記:

```python
from app.schemas import (
    ForecastRequest,
    ForecastResponse,
    ForecastPoint,
    ESTIMATION_WINDOW,
)


def test_forecast_request_valid():
    req = ForecastRequest(
        stock_code="7203",
        current_price=3250.0,
        prices=[3000.0 + i for i in range(ESTIMATION_WINDOW + 1)],
    )
    assert req.stock_code == "7203"
    assert len(req.prices) == ESTIMATION_WINDOW + 1


def test_forecast_request_too_few_prices():
    with pytest.raises(ValidationError) as exc_info:
        ForecastRequest(
            stock_code="7203",
            current_price=3000.0,
            prices=[3000.0] * ESTIMATION_WINDOW,
        )
    errors = exc_info.value.errors()
    assert any("prices" in str(e["loc"]) for e in errors)


def test_forecast_response_shape():
    resp = ForecastResponse(
        stock_code="7203",
        horizon=20,
        points=[
            ForecastPoint(
                step=1,
                center=3260.0,
                upper_68=3300.0,
                lower_68=3220.0,
                upper_95=3350.0,
                lower_95=3180.0,
            )
        ],
    )
    assert resp.horizon == 20
    assert resp.points[0].step == 1
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd python-engine && uv run pytest tests/test_schemas.py -v`
Expected: FAIL（`ForecastRequest`等が存在しない、`ImportError`）

- [ ] **Step 3: `app/schemas.py` にスキーマを追加する**

`python-engine/app/schemas.py` の末尾に追記:

```python
ESTIMATION_WINDOW = 120
FORECAST_HORIZON = 20


class ForecastRequest(BaseModel):
    stock_code: str
    current_price: float
    prices: list[float]

    @field_validator("prices")
    @classmethod
    def prices_min_length(cls, v: list[float]) -> list[float]:
        min_len = ESTIMATION_WINDOW + 1
        if len(v) < min_len:
            raise ValueError(f"prices must have at least {min_len} elements")
        return v


class ForecastPoint(BaseModel):
    step: int
    center: float
    upper_68: float
    lower_68: float
    upper_95: float
    lower_95: float


class ForecastResponse(BaseModel):
    stock_code: str
    horizon: int
    points: list[ForecastPoint]
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd python-engine && uv run pytest tests/test_schemas.py -v`
Expected: PASS

- [ ] **Step 5: 失敗するテストを書く（計算ロジック）**

`python-engine/tests/test_forecast_service.py` を新規作成:

```python
import math

import pytest

from app.schemas import ESTIMATION_WINDOW, FORECAST_HORIZON
from app.services.forecast import calculate_forecast


def test_calculate_forecast_shape():
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]
    current_price = prices[-1]

    result = calculate_forecast("7203", current_price, prices)

    assert result.stock_code == "7203"
    assert result.horizon == FORECAST_HORIZON
    assert len(result.points) == FORECAST_HORIZON
    assert [p.step for p in result.points] == list(range(1, FORECAST_HORIZON + 1))


def test_calculate_forecast_constant_growth_has_zero_width_band():
    """日次リターンが完全に一定（ボラ0）なら、中心線と68%/95%帯は一致する。"""
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]
    current_price = prices[-1]

    result = calculate_forecast("7203", current_price, prices)
    first = result.points[0]

    expected_center = current_price * 1.01
    assert first.center == pytest.approx(expected_center, rel=1e-6)
    assert first.upper_68 == pytest.approx(expected_center, rel=1e-6)
    assert first.lower_68 == pytest.approx(expected_center, rel=1e-6)
    assert first.upper_95 == pytest.approx(expected_center, rel=1e-6)
    assert first.lower_95 == pytest.approx(expected_center, rel=1e-6)


def test_calculate_forecast_band_widens_with_horizon():
    """ボラがある場合、帯の幅は sqrt(h) に比例して遠い将来ほど広がる。"""
    prices = [100.0 + i + (5.0 if i % 2 == 0 else -5.0) for i in range(ESTIMATION_WINDOW + 1)]
    current_price = prices[-1]

    result = calculate_forecast("7203", current_price, prices)
    width_h1 = result.points[0].upper_95 - result.points[0].lower_95
    width_h20 = result.points[19].upper_95 - result.points[19].lower_95

    assert width_h20 > width_h1
```

- [ ] **Step 6: テストが失敗することを確認**

Run: `cd python-engine && uv run pytest tests/test_forecast_service.py -v`
Expected: FAIL（`app.services.forecast` モジュールが存在しない）

- [ ] **Step 7: `app/services/forecast.py` を実装する**

```python
import math

from app.schemas import ESTIMATION_WINDOW, FORECAST_HORIZON, ForecastPoint, ForecastResponse

# 68%区間・95%区間のz値。正規分布の標準的な信頼区間の目安として採用。
_Z_68 = 1.0
_Z_95 = 1.96


def calculate_forecast(
    stock_code: str, current_price: float, prices: list[float]
) -> ForecastResponse:
    """
    直近 ESTIMATION_WINDOW 営業日の対数リターンから、ドリフト付きランダムウォークで
    FORECAST_HORIZON 営業日先までの中心線と68%/95%レンジ帯を計算する。

    prices: 古い順（index 0 が最古）の終値リスト。最低 ESTIMATION_WINDOW + 1 件。
    """
    window = prices[-(ESTIMATION_WINDOW + 1) :]
    log_returns = [math.log(window[i] / window[i - 1]) for i in range(1, len(window))]

    mu = sum(log_returns) / len(log_returns)
    variance = sum((r - mu) ** 2 for r in log_returns) / len(log_returns)
    sigma = math.sqrt(variance)

    points: list[ForecastPoint] = []
    for h in range(1, FORECAST_HORIZON + 1):
        drift = mu * h
        band = sigma * math.sqrt(h)
        points.append(
            ForecastPoint(
                step=h,
                center=current_price * math.exp(drift),
                upper_68=current_price * math.exp(drift + _Z_68 * band),
                lower_68=current_price * math.exp(drift - _Z_68 * band),
                upper_95=current_price * math.exp(drift + _Z_95 * band),
                lower_95=current_price * math.exp(drift - _Z_95 * band),
            )
        )

    return ForecastResponse(stock_code=stock_code, horizon=FORECAST_HORIZON, points=points)
```

- [ ] **Step 8: テストが通ることを確認**

Run: `cd python-engine && uv run pytest tests/test_forecast_service.py tests/test_schemas.py -v`
Expected: PASS（全件）

- [ ] **Step 9: コミット**

```bash
cd python-engine
git add app/schemas.py app/services/forecast.py tests/test_schemas.py tests/test_forecast_service.py
git commit -m "feat: add forecast calculation service and schemas"
```

---

### Task 2: Python — `POST /forecast` エンドポイント

**Files:**
- Create: `python-engine/app/routers/forecast.py`
- Modify: `python-engine/app/main.py`
- Test: `python-engine/tests/test_forecast_endpoint.py`

**Interfaces:**
- Consumes: Task 1の `ForecastRequest`/`ForecastResponse`/`calculate_forecast`
- Produces: `POST /forecast`（Go側 Task 3が呼ぶ）

- [ ] **Step 1: 失敗するテストを書く**

`python-engine/tests/test_forecast_endpoint.py` を新規作成:

```python
import pytest
from fastapi.testclient import TestClient
from app.main import app
from app.schemas import ESTIMATION_WINDOW


@pytest.fixture(scope="module")
def client():
    with TestClient(app) as c:
        yield c


def test_forecast_success(client):
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]
    response = client.post(
        "/forecast",
        json={"stock_code": "7203", "current_price": prices[-1], "prices": prices},
    )
    assert response.status_code == 200
    body = response.json()
    assert body["stock_code"] == "7203"
    assert body["horizon"] == 20
    assert len(body["points"]) == 20
    assert body["points"][0]["step"] == 1


def test_forecast_too_few_prices_returns_422(client):
    response = client.post(
        "/forecast",
        json={
            "stock_code": "7203",
            "current_price": 3000.0,
            "prices": [3000.0] * ESTIMATION_WINDOW,
        },
    )
    assert response.status_code == 422


def test_health_still_works(client):
    response = client.get("/health")
    assert response.status_code == 200
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd python-engine && uv run pytest tests/test_forecast_endpoint.py -v`
Expected: FAIL（404 Not Found、`/forecast` ルートが存在しない）

- [ ] **Step 3: ルーターを実装する**

`python-engine/app/routers/forecast.py` を新規作成:

```python
from fastapi import APIRouter
from app.schemas import ForecastRequest, ForecastResponse
from app.services.forecast import calculate_forecast

router = APIRouter()


@router.post("/forecast", response_model=ForecastResponse)
def forecast(req: ForecastRequest) -> ForecastResponse:
    return calculate_forecast(req.stock_code, req.current_price, req.prices)
```

`python-engine/app/main.py` を編集:

```python
from fastapi import FastAPI
from app.routers import analyze as analyze_router
from app.routers import forecast as forecast_router

app = FastAPI()
app.include_router(analyze_router.router)
app.include_router(forecast_router.router)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd python-engine && uv run pytest -v`
Expected: PASS（全件、既存の `/analyze` テストも含めて回帰なし）

- [ ] **Step 5: コミット**

```bash
cd python-engine
git add app/routers/forecast.py app/main.py tests/test_forecast_endpoint.py
git commit -m "feat: add POST /forecast endpoint"
```

---

### Task 3: Go — `domain/forecast` パッケージと `PythonEngineClient.Forecast`

**Files:**
- Create: `go-api/internal/domain/forecast/forecaster.go`
- Modify: `go-api/internal/interface/gateway/python_engine_client.go`
- Test: `go-api/internal/interface/gateway/python_engine_client_test.go`（追記）

**Interfaces:**
- Consumes: `stock.StockCode`
- Produces: `forecast.Point{Step int, Center, Upper68, Lower68, Upper95, Lower95 float64}`、`forecast.Forecast{Horizon int, Points []Point}`、`forecast.Forecaster interface { Forecast(code stock.StockCode, currentPrice float64, prices []float64) (Forecast, error) }`、`(*gateway.PythonEngineClient).Forecast(...)`（`forecast.Forecaster`を満たす）。Task 5がこのインターフェースに依存する。

- [ ] **Step 1: `domain/forecast` パッケージを作成する**

`go-api/internal/domain/forecast/forecaster.go` を新規作成:

```go
package forecast

import "github.com/stock-anomaly-detection/go-api/internal/domain/stock"

type Point struct {
	Step    int
	Center  float64
	Upper68 float64
	Lower68 float64
	Upper95 float64
	Lower95 float64
}

type Forecast struct {
	Horizon int
	Points  []Point
}

// Forecaster は将来の価格レンジ（ドリフト＋ボラティリティ帯）を計算する。
// 点予測ではなく方向とレンジ帯を返す方針は
// docs/superpowers/specs/2026-09-24-stock-chart-forecast-design.md 参照。
type Forecaster interface {
	Forecast(code stock.StockCode, currentPrice float64, prices []float64) (Forecast, error)
}
```

- [ ] **Step 2: 失敗するテストを書く（gatewayクライアント）**

`go-api/internal/interface/gateway/python_engine_client_test.go` の末尾に追記:

```go
func TestPythonEngineClient_Forecast(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /forecast", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "7203", body["stock_code"])
		assert.Equal(t, 3250.0, body["current_price"])

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"stock_code": "7203",
			"horizon":    20,
			"points": []map[string]interface{}{
				{"step": 1, "center": 3260.0, "upper_68": 3300.0, "lower_68": 3220.0, "upper_95": 3350.0, "lower_95": 3180.0},
			},
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	prices := make([]float64, 121)
	for i := range prices {
		prices[i] = 3000.0 + float64(i)
	}

	got, err := client.Forecast(code, 3250.0, prices)
	require.NoError(t, err)
	assert.Equal(t, 20, got.Horizon)
	require.Len(t, got.Points, 1)
	assert.Equal(t, 1, got.Points[0].Step)
	assert.InDelta(t, 3260.0, got.Points[0].Center, 0.001)
	assert.InDelta(t, 3220.0, got.Points[0].Lower68, 0.001)
}

func TestPythonEngineClient_Forecast_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /forecast", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.Forecast(code, 3250.0, make([]float64, 121))
	require.Error(t, err)
}
```

- [ ] **Step 3: テストが失敗することを確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestPythonEngineClient_Forecast -v`
Expected: FAIL（`client.Forecast` が存在せずコンパイルエラー）

- [ ] **Step 4: `PythonEngineClient.Forecast` を実装する**

`go-api/internal/interface/gateway/python_engine_client.go` のimportに `"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"` を追加し、ファイル末尾に追記:

```go
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

func (c *PythonEngineClient) Forecast(code stock.StockCode, currentPrice float64, prices []float64) (forecast.Forecast, error) {
	reqBody, err := json.Marshal(forecastRequest{
		StockCode:    code.String(),
		CurrentPrice: currentPrice,
		Prices:       prices,
	})
	if err != nil {
		return forecast.Forecast{}, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/forecast", bytes.NewReader(reqBody))
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
```

- [ ] **Step 5: テストが通ることを確認**

Run: `cd go-api && go test -race -short ./internal/interface/gateway/... -v`
Expected: PASS（全件）

- [ ] **Step 6: ビルド確認とコミット**

```bash
cd go-api
go build ./...
git add internal/domain/forecast/forecaster.go internal/interface/gateway/python_engine_client.go internal/interface/gateway/python_engine_client_test.go
git commit -m "feat: add forecast domain type and python engine forecast client"
```

---

### Task 4: Go — `notification.Repository` に `FindByStockCode` を追加

**Files:**
- Modify: `go-api/internal/domain/notification/repository.go`
- Modify: `go-api/internal/infrastructure/persistence/notification_repository.go`
- Test: `go-api/internal/infrastructure/persistence/notification_repository_test.go`（追記）
- Modify: `go-api/internal/usecase/analyze_and_notify_test.go`（既存モックにメソッド追加）
- Modify: `go-api/internal/usecase/monitor_notify_test.go`（既存モックにメソッド追加）

**Interfaces:**
- Consumes: 既存の `notification.Notification` エンティティ
- Produces: `notification.Repository.FindByStockCode(ctx, stockCode string) ([]Notification, error)`（通知が0件でもエラーにせず空スライスを返す）。Task 5が使う。

**注意（既存コードへの影響）:** `notification.Repository` インターフェースに `FindByStockCode` を追加すると、このインターフェースを実装している既存の2つのテスト用モック
（`go-api/internal/usecase/analyze_and_notify_test.go` の `MockNotificationRepository`＝`package usecase_test`、
`go-api/internal/usecase/monitor_notify_test.go` の `MockNotificationRepository`＝`package usecase`。
名前は同じだが別パッケージなので衝突はしない）が、そのままではインターフェースを満たせなくなり
`go build`/`go test`がコンパイルエラーになる。Step 4の直後でこの2ファイルに `FindByStockCode` の
スタブ実装を追加する（Step 5として追加）。

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/infrastructure/persistence/notification_repository_test.go` の末尾に追記:

```go
func TestPgNotificationRepository_FindByStockCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := persistence.Connect(ctx, databaseURL)
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Exec(ctx, "TRUNCATE notifications CASCADE")
	require.NoError(t, err)

	repo := persistence.NewPgNotificationRepository(conn)
	rsi := 65.5
	require.NoError(t, repo.Save(ctx, notification.Notification{
		StockCode:           "7203",
		AnomalyScore:        3.2,
		AIReport:            "テストレポート",
		TechnicalIndicators: analysis.Indicators{RSI: &rsi},
		SlackSent:           true,
	}))
	require.NoError(t, repo.Save(ctx, notification.Notification{
		StockCode:    "9984",
		AnomalyScore: 2.8,
	}))

	found, err := repo.FindByStockCode(ctx, "7203")
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "7203", found[0].StockCode)
	assert.Equal(t, "テストレポート", found[0].AIReport)
	require.NotNil(t, found[0].TechnicalIndicators.RSI)
	assert.InDelta(t, 65.5, *found[0].TechnicalIndicators.RSI, 0.001)
	assert.True(t, found[0].SlackSent)
	assert.False(t, found[0].NotifiedAt.IsZero())
}

func TestPgNotificationRepository_FindByStockCode_NoResults(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := persistence.Connect(ctx, databaseURL)
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Exec(ctx, "TRUNCATE notifications CASCADE")
	require.NoError(t, err)

	repo := persistence.NewPgNotificationRepository(conn)
	found, err := repo.FindByStockCode(ctx, "0000")
	require.NoError(t, err)
	assert.Empty(t, found)
}
```

このファイルの先頭importに `"github.com/stretchr/testify/assert"` を追加する（既存は `require` のみ）。

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd go-api && DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test -race ./internal/infrastructure/persistence/... -run TestPgNotificationRepository_FindByStockCode -v`
Expected: FAIL（`repo.FindByStockCode` が存在せずコンパイルエラー）

- [ ] **Step 3: `notification.Repository` インターフェースに追加する**

`go-api/internal/domain/notification/repository.go` を編集:

```go
package notification

import "context"

type Repository interface {
	Save(ctx context.Context, n Notification) error
	// FindByStockCode はstockCodeに紐づく通知履歴を notified_at 昇順で返す。
	// 該当が無ければ空スライスを返す（エラーにしない）。
	FindByStockCode(ctx context.Context, stockCode string) ([]Notification, error)
}
```

- [ ] **Step 4: Postgres実装を追加する**

`go-api/internal/infrastructure/persistence/notification_repository.go` のimportに `"database/sql"` を追加し、末尾に追記:

```go
func (r *PgNotificationRepository) FindByStockCode(ctx context.Context, stockCode string) ([]notification.Notification, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT user_id, stock_code, anomaly_score, ai_report, technical_indicators, slack_sent, notified_at
		 FROM notifications
		 WHERE stock_code = $1
		 ORDER BY notified_at ASC`,
		stockCode)
	if err != nil {
		return nil, fmt.Errorf("find notifications %s: %w", stockCode, err)
	}
	defer rows.Close()

	notifications := []notification.Notification{}
	for rows.Next() {
		var n notification.Notification
		var userID sql.NullString
		var aiReport sql.NullString
		var indicatorsJSON []byte
		if err := rows.Scan(&userID, &n.StockCode, &n.AnomalyScore, &aiReport, &indicatorsJSON, &n.SlackSent, &n.NotifiedAt); err != nil {
			return nil, fmt.Errorf("scan notification %s: %w", stockCode, err)
		}
		if userID.Valid {
			n.UserID = &userID.String
		}
		n.AIReport = aiReport.String
		if len(indicatorsJSON) > 0 {
			if err := json.Unmarshal(indicatorsJSON, &n.TechnicalIndicators); err != nil {
				return nil, fmt.Errorf("unmarshal technical indicators %s: %w", stockCode, err)
			}
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find notifications %s: %w", stockCode, err)
	}
	return notifications, nil
}
```

- [ ] **Step 5: テストが通ることを確認**

Run: `cd go-api && DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test -race ./internal/infrastructure/persistence/... -run TestPgNotificationRepository -v`
Expected: PASS（全件）

ローカルにテストDBが無い場合は `testing.Short()` によりスキップされる（`go test -race -short ./...`）。CI（postgres:16サービス）で実行される。

- [ ] **Step 6: 既存モックを修正してビルドを回復する**

この時点で `cd go-api && go build ./... && go vet ./...` を実行すると、`internal/usecase/analyze_and_notify_test.go` と
`internal/usecase/monitor_notify_test.go` それぞれの `MockNotificationRepository` が
`notification.Repository` を満たせずコンパイルエラーになることを確認する（意図した一時的な破損）。

`go-api/internal/usecase/analyze_and_notify_test.go` の既存の
`func (m *MockNotificationRepository) Save(ctx context.Context, n notification.Notification) error { ... }`
の直後に追記:

```go
func (m *MockNotificationRepository) FindByStockCode(ctx context.Context, stockCode string) ([]notification.Notification, error) {
	args := m.Called(ctx, stockCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]notification.Notification), args.Error(1)
}
```

`go-api/internal/usecase/monitor_notify_test.go` の既存の
`func (m *MockNotificationRepository) Save(ctx context.Context, n notification.Notification) error { ... }`
の直後にも同じメソッドを追記する（このファイルは `package usecase` だが中身は同一でよい）。

- [ ] **Step 7: ビルドと全体テストが回復したことを確認**

Run: `cd go-api && go build ./... && go vet ./... && go test -race -short ./...`
Expected: PASS（全件、既存の `analyze_and_notify_test.go`・`monitor_notify_test.go` のテストも含めて回帰なし）

- [ ] **Step 8: コミット**

```bash
cd go-api
git add internal/domain/notification/repository.go internal/infrastructure/persistence/notification_repository.go internal/infrastructure/persistence/notification_repository_test.go internal/usecase/analyze_and_notify_test.go internal/usecase/monitor_notify_test.go
git commit -m "feat: add FindByStockCode to notification repository"
```

---

### Task 5: Go — `GetStockChartUsecase`

**Files:**
- Create: `go-api/internal/usecase/get_stock_chart.go`
- Test: `go-api/internal/usecase/get_stock_chart_test.go`
- Test: `go-api/internal/usecase/mock_forecaster_test.go`

**Interfaces:**
- Consumes: `watchlist.Repository.FindByUserID`（既存）、`stock.PriceRepository.FindRecent`（既存）、`anomaly.DetectionService.Calculate`（既存、変更なし）、`forecast.Forecaster.Forecast`（Task 3）、`notification.Repository.FindByStockCode`（Task 4）
- Produces: `usecase.AlertBandPoint{Date string, Upper, Lower float64}`、`usecase.StockChart{StockCode string, CurrentPrice *float64, Prices []stock.Quote, AlertBand []AlertBandPoint, CurrentZScore *float64, Forecast *forecast.Forecast, Notifications []notification.Notification}`、`usecase.GetStockChartUsecase.Handle(ctx, userID, rawStockCode string) (StockChart, error)`（watchlistに無ければ `watchlist.ErrNotFound`、不正な証券コードなら `stock.ErrInvalidStockCode`）。Task 6が使う。

**注意（既存モックの再利用）:** `notification.Repository` のモックは新規作成しない。
`go-api/internal/usecase/analyze_and_notify_test.go`（`package usecase_test`、このタスクのテストファイルと同じパッケージ）に
既に `MockNotificationRepository` が定義されており、Task 4で `FindByStockCode` も追加済みなのでそのまま使える。
同じパッケージ内での型の重複定義を避けるため、このタスクで新しい `MockNotificationRepository` は作らないこと。

- [ ] **Step 1: モックを追加する**

`go-api/internal/usecase/mock_forecaster_test.go` を新規作成:

```go
package usecase_test

import (
	"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/mock"
)

type MockForecaster struct{ mock.Mock }

func (m *MockForecaster) Forecast(code stock.StockCode, currentPrice float64, prices []float64) (forecast.Forecast, error) {
	args := m.Called(code, currentPrice, prices)
	return args.Get(0).(forecast.Forecast), args.Error(1)
}
```

`notification.Repository` 用のモックは新規作成しない。`mockWatchlistRepository`（`manage_watchlist_test.go`）、
`MockPriceRepository`（`mock_price_repository_test.go`）、`MockNotificationRepository`（`analyze_and_notify_test.go`、
Task 4で`FindByStockCode`を追加済み）はいずれも同じ `usecase_test` パッケージ内の既存実装をそのまま再利用する。

- [ ] **Step 2: 失敗するテストを書く**

`go-api/internal/usecase/get_stock_chart_test.go` を新規作成:

```go
package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildQuotes(n int) []stock.Quote {
	quotes := make([]stock.Quote, n)
	for i := 0; i < n; i++ {
		quotes[i] = stock.Quote{Price: stock.Price(1000 + float64(i)), Date: fmt.Sprintf("day-%03d", i)}
	}
	return quotes
}

func newChartUsecase(t *testing.T, watchlists *mockWatchlistRepository, prices *MockPriceRepository, forecaster *MockForecaster, notifications *MockNotificationRepository) *usecase.GetStockChartUsecase {
	t.Helper()
	return usecase.NewGetStockChartUsecase(watchlists, prices, anomaly.NewDetectionService(), forecaster, notifications, 2.5)
}

func TestGetStockChartUsecase_Success(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: code}}, nil)

	quotes := buildQuotes(130)
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return(quotes, nil)

	forecaster := new(MockForecaster)
	forecaster.On("Forecast", code, mock.Anything, mock.Anything).
		Return(forecast.Forecast{Horizon: 20, Points: []forecast.Point{{Step: 1, Center: 1200}}}, nil)

	notifications := new(MockNotificationRepository)
	notifications.On("FindByStockCode", mock.Anything, "7203").
		Return([]notification.Notification{{StockCode: "7203", AnomalyScore: 3.1}}, nil)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	chart, err := uc.Handle(context.Background(), "user-1", "7203")

	require.NoError(t, err)
	assert.Equal(t, "7203", chart.StockCode)
	require.NotNil(t, chart.CurrentPrice)
	assert.InDelta(t, 1129.0, *chart.CurrentPrice, 0.001)

	// アラート帯: window=30なので index 29..129 の101件、先頭は history=quotes[0:29]（0..28の29整数）から計算
	require.Len(t, chart.AlertBand, 101)
	assert.Equal(t, "day-029", chart.AlertBand[0].Date)
	assert.InDelta(t, 1034.9165, chart.AlertBand[0].Upper, 0.01)
	assert.InDelta(t, 993.0835, chart.AlertBand[0].Lower, 0.01)

	require.NotNil(t, chart.CurrentZScore)
	assert.InDelta(t, 1.79284, float64(*chart.CurrentZScore), 0.01)

	require.NotNil(t, chart.Forecast)
	assert.Equal(t, 20, chart.Forecast.Horizon)
	require.Len(t, chart.Forecast.Points, 1)
	assert.InDelta(t, 1200.0, chart.Forecast.Points[0].Center, 0.001)

	require.Len(t, chart.Notifications, 1)
	assert.Equal(t, "7203", chart.Notifications[0].StockCode)
}

func TestGetStockChartUsecase_InsufficientHistory_ForecastAndZScoreNil(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: code}}, nil)

	quotes := buildQuotes(20)
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return(quotes, nil)

	// forecaster には期待を設定しない: 呼ばれたらtestifyのmockが未設定呼び出しとして失敗させる
	forecaster := new(MockForecaster)
	notifications := new(MockNotificationRepository)
	notifications.On("FindByStockCode", mock.Anything, "7203").Return([]notification.Notification{}, nil)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	chart, err := uc.Handle(context.Background(), "user-1", "7203")

	require.NoError(t, err)
	require.NotNil(t, chart.CurrentPrice)
	assert.Empty(t, chart.AlertBand)
	assert.Nil(t, chart.CurrentZScore)
	assert.Nil(t, chart.Forecast)
	forecaster.AssertNotCalled(t, "Forecast", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetStockChartUsecase_ForecasterError_ForecastNilButNoError(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: code}}, nil)

	quotes := buildQuotes(130)
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return(quotes, nil)

	forecaster := new(MockForecaster)
	forecaster.On("Forecast", code, mock.Anything, mock.Anything).
		Return(forecast.Forecast{}, errors.New("python engine down"))

	notifications := new(MockNotificationRepository)
	notifications.On("FindByStockCode", mock.Anything, "7203").Return([]notification.Notification{}, nil)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	chart, err := uc.Handle(context.Background(), "user-1", "7203")

	require.NoError(t, err)
	assert.Nil(t, chart.Forecast)
	require.NotNil(t, chart.CurrentZScore)
}

func TestGetStockChartUsecase_NotInWatchlist_ReturnsNotFound(t *testing.T) {
	otherCode, _ := stock.NewStockCode("9984")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: otherCode}}, nil)

	prices := new(MockPriceRepository)
	forecaster := new(MockForecaster)
	notifications := new(MockNotificationRepository)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	_, err := uc.Handle(context.Background(), "user-1", "7203")

	require.Error(t, err)
	assert.ErrorIs(t, err, watchlist.ErrNotFound)
	prices.AssertNotCalled(t, "FindRecent", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetStockChartUsecase_InvalidStockCode_ReturnsError(t *testing.T) {
	watchlists := new(mockWatchlistRepository)
	prices := new(MockPriceRepository)
	forecaster := new(MockForecaster)
	notifications := new(MockNotificationRepository)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	_, err := uc.Handle(context.Background(), "user-1", "12")

	require.Error(t, err)
	assert.ErrorIs(t, err, stock.ErrInvalidStockCode)
	watchlists.AssertNotCalled(t, "FindByUserID", mock.Anything, mock.Anything)
}
```

このファイルの先頭importに `"github.com/stretchr/testify/mock"` を追加する。

- [ ] **Step 3: テストが失敗することを確認**

Run: `cd go-api && go test -race -short ./internal/usecase/... -run TestGetStockChartUsecase -v`
Expected: FAIL（`usecase.GetStockChartUsecase`/`usecase.NewGetStockChartUsecase` が存在せずコンパイルエラー）

- [ ] **Step 4: `GetStockChartUsecase` を実装する**

`go-api/internal/usecase/get_stock_chart.go` を新規作成:

```go
package usecase

import (
	"context"
	"math"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

// forecastMinPrices はPython engineの /forecast が要求する最低価格件数
// （推定ウィンドウ120営業日＋1）。これ未満の場合はPython engineを呼ばずforecastをnilにする。
const forecastMinPrices = 121

type AlertBandPoint struct {
	Date  string
	Upper float64
	Lower float64
}

type StockChart struct {
	StockCode     string
	CurrentPrice  *float64
	Prices        []stock.Quote
	AlertBand     []AlertBandPoint
	CurrentZScore *float64
	Forecast      *forecast.Forecast
	Notifications []notification.Notification
}

type GetStockChartUsecase struct {
	watchlists    watchlist.Repository
	prices        stock.PriceRepository
	detector      *anomaly.DetectionService
	forecaster    forecast.Forecaster
	notifications notification.Repository
	threshold     float64
}

func NewGetStockChartUsecase(
	watchlists watchlist.Repository,
	prices stock.PriceRepository,
	detector *anomaly.DetectionService,
	forecaster forecast.Forecaster,
	notifications notification.Repository,
	threshold float64,
) *GetStockChartUsecase {
	return &GetStockChartUsecase{
		watchlists:    watchlists,
		prices:        prices,
		detector:      detector,
		forecaster:    forecaster,
		notifications: notifications,
		threshold:     threshold,
	}
}

// Handle はuserIDのwatchlistにrawStockCodeが存在する場合のみチャートデータを返す。
// watchlistに無い銘柄は watchlist.ErrNotFound を返す（データの存在範囲と認可範囲を一致させるため、
// Yahooへの追加リクエスト経路を作らないため）。予測やzスコアに必要な件数の履歴が無い場合、
// または Python engine呼び出しが失敗した場合は該当フィールドを nil にするだけでエラーにはしない。
func (u *GetStockChartUsecase) Handle(ctx context.Context, userID, rawStockCode string) (StockChart, error) {
	code, err := stock.NewStockCode(rawStockCode)
	if err != nil {
		return StockChart{}, err
	}

	items, err := u.watchlists.FindByUserID(ctx, userID)
	if err != nil {
		return StockChart{}, err
	}
	owned := false
	for _, item := range items {
		if item.StockCode == code {
			owned = true
			break
		}
	}
	if !owned {
		return StockChart{}, watchlist.ErrNotFound
	}

	quotes, err := u.prices.FindRecent(ctx, code, backfillDays)
	if err != nil {
		return StockChart{}, err
	}

	chart := StockChart{
		StockCode: code.String(),
		Prices:    quotes,
		AlertBand: computeAlertBand(quotes, historySize, u.threshold),
	}

	if len(quotes) > 0 {
		current := float64(quotes[len(quotes)-1].Price)
		chart.CurrentPrice = &current
	}

	if len(quotes) >= historySize {
		floatPrices := make([]float64, historySize)
		for i, q := range quotes[len(quotes)-historySize:] {
			floatPrices[i] = float64(q.Price)
		}
		if z, err := u.detector.Calculate(floatPrices); err == nil {
			zf := float64(z)
			chart.CurrentZScore = &zf
		}
	}

	if len(quotes) >= forecastMinPrices && chart.CurrentPrice != nil {
		floatPrices := make([]float64, len(quotes))
		for i, q := range quotes {
			floatPrices[i] = float64(q.Price)
		}
		if f, err := u.forecaster.Forecast(code, *chart.CurrentPrice, floatPrices); err == nil {
			chart.Forecast = &f
		}
	}

	if notifications, err := u.notifications.FindByStockCode(ctx, code.String()); err == nil {
		chart.Notifications = notifications
	}

	return chart, nil
}

// computeAlertBand は各時点について、その時点を「現在値」とみなしたときの
// anomaly.DetectionService.Calculate と同じ窓（末尾が現在値、先頭〜末尾-1が履歴）を使って
// アラート境界帯（履歴の平均±threshold×標準偏差）を計算する。window件に満たない
// 先頭部分は帯を計算できないため結果に含めない。anomaly/service.go 自体は変更しない。
func computeAlertBand(quotes []stock.Quote, window int, threshold float64) []AlertBandPoint {
	band := []AlertBandPoint{}
	for i := window - 1; i < len(quotes); i++ {
		history := quotes[i-window+1 : i]

		mean := 0.0
		for _, q := range history {
			mean += float64(q.Price)
		}
		mean /= float64(len(history))

		variance := 0.0
		for _, q := range history {
			diff := float64(q.Price) - mean
			variance += diff * diff
		}
		stddev := math.Sqrt(variance / float64(len(history)))

		band = append(band, AlertBandPoint{
			Date:  quotes[i].Date,
			Upper: mean + threshold*stddev,
			Lower: mean - threshold*stddev,
		})
	}
	return band
}
```

- [ ] **Step 5: テストが通ることを確認**

Run: `cd go-api && go test -race -short ./internal/usecase/... -v`
Expected: PASS（全件、既存usecaseテストも含めて回帰なし）

- [ ] **Step 6: ビルド確認とコミット**

```bash
cd go-api
go build ./...
git add internal/usecase/get_stock_chart.go internal/usecase/get_stock_chart_test.go internal/usecase/mock_forecaster_test.go
git commit -m "feat: add GetStockChartUsecase combining prices, alert band, forecast, and notifications"
```

---

### Task 6: Go — `StockHandler` とルーティング配線

**Files:**
- Create: `go-api/internal/interface/handler/stock_handler.go`
- Test: `go-api/internal/interface/handler/stock_handler_test.go`
- Modify: `go-api/cmd/api/main.go`

**Interfaces:**
- Consumes: Task 5の `usecase.StockChart`/`usecase.GetStockChartUsecase`
- Produces: `GET /stocks/{code}/chart`（認証必須、frontend Task 7が呼ぶ）

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/interface/handler/stock_handler_test.go` を新規作成:

```go
package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubStockChartUsecase struct {
	result usecase.StockChart
	err    error
}

func (s stubStockChartUsecase) Handle(ctx context.Context, userID, stockCode string) (usecase.StockChart, error) {
	return s.result, s.err
}

func TestStockHandler_Chart_Success(t *testing.T) {
	price := 1200.0
	z := 1.5
	h := handler.NewStockHandler(stubStockChartUsecase{result: usecase.StockChart{
		StockCode:     "7203",
		CurrentPrice:  &price,
		CurrentZScore: &z,
		Prices:        []stock.Quote{{Date: "2026-01-01", Price: 1200}},
		AlertBand:     []usecase.AlertBandPoint{},
		Notifications: []notification.Notification{},
	}})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/stocks/7203/chart", nil), "user-1")
	req.SetPathValue("code", "7203")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "7203", resp["stock_code"])
	assert.InDelta(t, 1200.0, resp["current_price"], 0.001)
	assert.InDelta(t, 1.5, resp["current_z_score"], 0.001)
	assert.Nil(t, resp["forecast"])
}

func TestStockHandler_Chart_InvalidStockCode(t *testing.T) {
	h := handler.NewStockHandler(stubStockChartUsecase{err: stock.ErrInvalidStockCode})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/stocks/12/chart", nil), "user-1")
	req.SetPathValue("code", "12")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStockHandler_Chart_NotInWatchlist(t *testing.T) {
	h := handler.NewStockHandler(stubStockChartUsecase{err: watchlist.ErrNotFound})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/stocks/9999/chart", nil), "user-1")
	req.SetPathValue("code", "9999")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStockHandler_Chart_Unauthorized(t *testing.T) {
	h := handler.NewStockHandler(stubStockChartUsecase{})
	req := httptest.NewRequest(http.MethodGet, "/stocks/7203/chart", nil)
	req.SetPathValue("code", "7203")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd go-api && go test -race -short ./internal/interface/handler/... -run TestStockHandler -v`
Expected: FAIL（`handler.NewStockHandler`/`StockHandler.Chart` が存在せずコンパイルエラー）

- [ ] **Step 3: `StockHandler` を実装する**

`go-api/internal/interface/handler/stock_handler.go` を新規作成:

```go
package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
)

type stockChartUsecase interface {
	Handle(ctx context.Context, userID, stockCode string) (usecase.StockChart, error)
}

type StockHandler struct {
	charts stockChartUsecase
}

func NewStockHandler(charts stockChartUsecase) *StockHandler {
	return &StockHandler{charts: charts}
}

type priceResponse struct {
	Date  string  `json:"date"`
	Close float64 `json:"close"`
}

type alertBandResponse struct {
	Date  string  `json:"date"`
	Upper float64 `json:"upper"`
	Lower float64 `json:"lower"`
}

type forecastPointResponse struct {
	Step    int     `json:"step"`
	Center  float64 `json:"center"`
	Upper68 float64 `json:"upper_68"`
	Lower68 float64 `json:"lower_68"`
	Upper95 float64 `json:"upper_95"`
	Lower95 float64 `json:"lower_95"`
}

type forecastResponse struct {
	Horizon int                     `json:"horizon"`
	Points  []forecastPointResponse `json:"points"`
}

type notificationResponse struct {
	NotifiedAt   time.Time `json:"notified_at"`
	AnomalyScore float64   `json:"anomaly_score"`
	AIReport     string    `json:"ai_report"`
	SlackSent    bool      `json:"slack_sent"`
}

type stockChartResponse struct {
	StockCode     string                 `json:"stock_code"`
	CurrentPrice  *float64               `json:"current_price"`
	Prices        []priceResponse        `json:"prices"`
	AlertBand     []alertBandResponse    `json:"alert_band"`
	CurrentZScore *float64               `json:"current_z_score"`
	Forecast      *forecastResponse      `json:"forecast"`
	Notifications []notificationResponse `json:"notifications"`
}

func toStockChartResponse(c usecase.StockChart) stockChartResponse {
	prices := make([]priceResponse, len(c.Prices))
	for i, q := range c.Prices {
		prices[i] = priceResponse{Date: q.Date, Close: float64(q.Price)}
	}
	band := make([]alertBandResponse, len(c.AlertBand))
	for i, b := range c.AlertBand {
		band[i] = alertBandResponse{Date: b.Date, Upper: b.Upper, Lower: b.Lower}
	}
	notifications := make([]notificationResponse, len(c.Notifications))
	for i, n := range c.Notifications {
		notifications[i] = notificationResponse{
			NotifiedAt:   n.NotifiedAt,
			AnomalyScore: n.AnomalyScore,
			AIReport:     n.AIReport,
			SlackSent:    n.SlackSent,
		}
	}
	var fc *forecastResponse
	if c.Forecast != nil {
		points := make([]forecastPointResponse, len(c.Forecast.Points))
		for i, p := range c.Forecast.Points {
			points[i] = forecastPointResponse{
				Step: p.Step, Center: p.Center,
				Upper68: p.Upper68, Lower68: p.Lower68,
				Upper95: p.Upper95, Lower95: p.Lower95,
			}
		}
		fc = &forecastResponse{Horizon: c.Forecast.Horizon, Points: points}
	}
	return stockChartResponse{
		StockCode:     c.StockCode,
		CurrentPrice:  c.CurrentPrice,
		Prices:        prices,
		AlertBand:     band,
		CurrentZScore: c.CurrentZScore,
		Forecast:      fc,
		Notifications: notifications,
	}
}

func (h *StockHandler) Chart(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	code := r.PathValue("code")
	chart, err := h.charts.Handle(r.Context(), userID, code)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, toStockChartResponse(chart))
	case errors.Is(err, stock.ErrInvalidStockCode):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, watchlist.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd go-api && go test -race -short ./internal/interface/handler/... -v`
Expected: PASS（全件）

- [ ] **Step 5: `main.go` に配線する**

`go-api/cmd/api/main.go` を編集。`watchlistUsecase := usecase.NewManageWatchlistUsecase(...)` の直後に追加:

```go
	chartUsecase := usecase.NewGetStockChartUsecase(watchlistRepo, priceRepo, detector, pythonEngineClient, notificationRepo, threshold)
```

`watchlistHandler := handler.NewWatchlistHandler(watchlistUsecase)` の直後に追加:

```go
	stockHandler := handler.NewStockHandler(chartUsecase)
```

`mux.HandleFunc("PATCH /watchlist/{id}", ...)` の直後に追加:

```go
	mux.HandleFunc("GET /stocks/{code}/chart", handler.RequireAuth(tokenService, stockHandler.Chart))
```

- [ ] **Step 6: ビルド確認**

Run: `cd go-api && go build ./... && go vet ./...`
Expected: エラーなし

- [ ] **Step 7: 全体のGoテストを実行**

Run: `cd go-api && go test -race -short ./...`
Expected: PASS（全件、既存分含めて回帰なし）

- [ ] **Step 8: コミット**

```bash
cd go-api
git add internal/interface/handler/stock_handler.go internal/interface/handler/stock_handler_test.go cmd/api/main.go
git commit -m "feat: add GET /stocks/{code}/chart endpoint"
```

---

### Task 7: Frontend — `recharts` 依存追加と `fetchStockChart`

**Files:**
- Modify: `frontend/package.json`（`npm install recharts` で自動更新）
- Modify: `frontend/lib/go-api-client.ts`
- Test: `frontend/lib/go-api-client.test.ts`（追記）

**Interfaces:**
- Consumes: Go API Task 6の `GET /stocks/{code}/chart` レスポンス
- Produces: `StockChart`型一式（`StockChartPrice`, `StockChartAlertBand`, `StockChartForecastPoint`, `StockChartForecast`, `StockChartNotification`, `StockChart`）、`fetchStockChart(token, stockCode) -> {ok:true, chart} | ApiFailure`。Task 8・Task 9が使う。

- [ ] **Step 1: `recharts` をインストールする**

Run: `cd frontend && npm install recharts`
Expected: `package.json`の`dependencies`に`recharts`が追加される

- [ ] **Step 2: 失敗するテストを書く**

`frontend/lib/go-api-client.test.ts` のimportに `fetchStockChart` を追加し、`describe` ブロック内、既存の `fetchWatchlist` テストの後に追記:

```ts
  it("fetchStockChart returns chart data and sends bearer token", async () => {
    const chartBody = {
      stock_code: "7203",
      current_price: 3250.0,
      prices: [{ date: "2026-09-01", close: 3200.0 }],
      alert_band: [{ date: "2026-09-01", upper: 3300.0, lower: 3100.0 }],
      current_z_score: 1.5,
      forecast: {
        horizon: 20,
        points: [
          {
            step: 1,
            center: 3260.0,
            upper_68: 3300.0,
            lower_68: 3220.0,
            upper_95: 3350.0,
            lower_95: 3180.0,
          },
        ],
      },
      notifications: [],
    };
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify(chartBody), { status: 200 }),
    );

    const result = await fetchStockChart("jwt-token", "7203");

    expect(result).toEqual({ ok: true, chart: chartBody });
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/stocks/7203/chart",
      expect.objectContaining({
        headers: { Authorization: "Bearer jwt-token" },
      }),
    );
  });

  it("fetchStockChart returns error on 404", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "watchlist item not found" }), {
        status: 404,
      }),
    );

    const result = await fetchStockChart("jwt-token", "9999");

    expect(result).toEqual({
      ok: false,
      error: "watchlist item not found",
      status: 404,
    });
  });
```

- [ ] **Step 3: テストが失敗することを確認**

Run: `cd frontend && npm test -- go-api-client`
Expected: FAIL（`fetchStockChart` が存在せず型エラー/importエラー）

- [ ] **Step 4: `fetchStockChart` を実装する**

`frontend/lib/go-api-client.ts` の末尾に追記:

```ts
export type StockChartPrice = { date: string; close: number };
export type StockChartAlertBand = { date: string; upper: number; lower: number };
export type StockChartForecastPoint = {
  step: number;
  center: number;
  upper_68: number;
  lower_68: number;
  upper_95: number;
  lower_95: number;
};
export type StockChartForecast = {
  horizon: number;
  points: StockChartForecastPoint[];
};
export type StockChartNotification = {
  notified_at: string;
  anomaly_score: number;
  ai_report: string;
  slack_sent: boolean;
};
export type StockChart = {
  stock_code: string;
  current_price: number | null;
  prices: StockChartPrice[];
  alert_band: StockChartAlertBand[];
  current_z_score: number | null;
  forecast: StockChartForecast | null;
  notifications: StockChartNotification[];
};

export async function fetchStockChart(
  token: string,
  stockCode: string,
): Promise<{ ok: true; chart: StockChart } | ApiFailure> {
  const res = await fetch(`${getGoApiUrl()}/stocks/${stockCode}/chart`, {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (res.status === 200) {
    const chart = (await res.json()) as StockChart;
    return { ok: true, chart };
  }
  return toFailure(res);
}
```

- [ ] **Step 5: テストが通ることを確認**

Run: `cd frontend && npm test -- go-api-client`
Expected: PASS（全件）

- [ ] **Step 6: コミット**

```bash
cd frontend
git add package.json package-lock.json lib/go-api-client.ts lib/go-api-client.test.ts
git commit -m "feat: add recharts dependency and fetchStockChart client"
```

---

### Task 8: Frontend — `StockChart` コンポーネント（Recharts）

**Files:**
- Create: `frontend/components/stock-chart.tsx`
- Test: `frontend/components/stock-chart.test.tsx`

**Interfaces:**
- Consumes: Task 7の `StockChart`型
- Produces: `<StockChart data={StockChart} />`。Task 9が使う。

- [ ] **Step 1: 失敗するテストを書く**

`frontend/components/stock-chart.test.tsx` を新規作成:

```tsx
import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { render, screen } from "@testing-library/react";
import { StockChart } from "./stock-chart";
import type { StockChart as StockChartData } from "@/lib/go-api-client";

// jsdomは ResizeObserver を実装しておらず、実測レイアウト（offsetWidth/offsetHeight）も
// 常に0を返す。Rechartsの ResponsiveContainer はこれらが無いと中身（Legendなど）を
// 描画しないため、テスト用に最小限のモックと固定サイズを与える。
class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

let originalResizeObserver: typeof ResizeObserver | undefined;
let originalOffsetWidth: PropertyDescriptor | undefined;
let originalOffsetHeight: PropertyDescriptor | undefined;

beforeAll(() => {
  originalResizeObserver = global.ResizeObserver;
  originalOffsetWidth = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    "offsetWidth",
  );
  originalOffsetHeight = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    "offsetHeight",
  );

  // @ts-expect-error jsdom does not implement ResizeObserver
  global.ResizeObserver = ResizeObserverMock;
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    value: 600,
  });
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    value: 400,
  });
});

afterAll(() => {
  global.ResizeObserver = originalResizeObserver as typeof ResizeObserver;
  if (originalOffsetWidth) {
    Object.defineProperty(HTMLElement.prototype, "offsetWidth", originalOffsetWidth);
  }
  if (originalOffsetHeight) {
    Object.defineProperty(HTMLElement.prototype, "offsetHeight", originalOffsetHeight);
  }
});

const baseData: StockChartData = {
  stock_code: "7203",
  current_price: 3250.0,
  prices: [
    { date: "2026-09-01", close: 3200.0 },
    { date: "2026-09-02", close: 3250.0 },
  ],
  alert_band: [{ date: "2026-09-02", upper: 3400.0, lower: 3000.0 }],
  current_z_score: 1.5,
  forecast: {
    horizon: 20,
    points: [
      {
        step: 1,
        center: 3260.0,
        upper_68: 3300.0,
        lower_68: 3220.0,
        upper_95: 3350.0,
        lower_95: 3180.0,
      },
    ],
  },
  notifications: [
    {
      notified_at: "2026-09-02T07:00:00Z",
      anomaly_score: 3.1,
      ai_report: "急騰の背景には...",
      slack_sent: true,
    },
  ],
};

describe("StockChart", () => {
  it("renders without the unavailable-forecast notice when forecast exists", () => {
    render(<StockChart data={baseData} />);
    expect(
      screen.queryByText("予測を取得できませんでした"),
    ).not.toBeInTheDocument();
  });

  it("shows the unavailable-forecast notice when forecast is null", () => {
    render(<StockChart data={{ ...baseData, forecast: null }} />);
    expect(
      screen.getByText("予測を取得できませんでした"),
    ).toBeInTheDocument();
  });

  it("labels the alert band and the statistical range separately in the legend", async () => {
    render(<StockChart data={baseData} />);
    // ResponsiveContainerのサイズ確定（ResizeObserverコールバック）を待つため非同期クエリを使う。
    expect(await screen.findByText("アラート境界")).toBeInTheDocument();
    expect(await screen.findByText("統計的期待レンジ（68%）")).toBeInTheDocument();
    expect(await screen.findByText("統計的期待レンジ（95%）")).toBeInTheDocument();
  });

  it("shows the calculation basis note for the forecast range", () => {
    render(<StockChart data={baseData} />);
    expect(
      screen.getByText(/直近120営業日の対数リターンの平均と標準偏差/),
    ).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd frontend && npm test -- stock-chart`
Expected: FAIL（`./stock-chart` モジュールが存在しない）

- [ ] **Step 3: `StockChart` コンポーネントを実装する**

`frontend/components/stock-chart.tsx` を新規作成:

```tsx
"use client";

import {
  ComposedChart,
  Area,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceDot,
} from "recharts";
import type {
  StockChart as StockChartData,
  StockChartNotification,
} from "@/lib/go-api-client";

type ChartRow = {
  date: string;
  close?: number;
  // Rechartsの Area は dataKey の値が [min, max] の2要素配列だと、その範囲を帯として描画する
  // （0からの塗りつぶしにならない）。アラート境界・予測レンジ帯はこの仕組みで表現する。
  alertRange?: [number, number];
  forecastCenter?: number;
  forecast68Range?: [number, number];
  forecast95Range?: [number, number];
  notification?: StockChartNotification;
};

// 土日のみを非営業日として扱う簡易実装。日本の祝日カレンダーを持たないため、
// 祝日を挟む場合は予測線のX軸ラベルが実際の取引日と1日以上ずれうる（既知の限界）。
function addBusinessDays(dateStr: string, days: number): string {
  const d = new Date(`${dateStr}T00:00:00Z`);
  let added = 0;
  while (added < days) {
    d.setUTCDate(d.getUTCDate() + 1);
    const day = d.getUTCDay();
    if (day !== 0 && day !== 6) added++;
  }
  return d.toISOString().slice(0, 10);
}

function buildRows(data: StockChartData): ChartRow[] {
  const alertByDate = new Map(data.alert_band.map((b) => [b.date, b]));
  // 同一日に複数回発火した場合は最新の通知を優先する（末尾優先でMapに詰める）。
  const notificationByDate = new Map(
    data.notifications.map((n) => [n.notified_at.slice(0, 10), n]),
  );
  const rows: ChartRow[] = data.prices.map((p) => {
    const band = alertByDate.get(p.date);
    return {
      date: p.date,
      close: p.close,
      alertRange: band ? [band.lower, band.upper] : undefined,
      notification: notificationByDate.get(p.date),
    };
  });

  if (data.forecast && data.prices.length > 0) {
    const lastDate = data.prices[data.prices.length - 1].date;
    for (const point of data.forecast.points) {
      rows.push({
        date: addBusinessDays(lastDate, point.step),
        forecastCenter: point.center,
        forecast68Range: [point.lower_68, point.upper_68],
        forecast95Range: [point.lower_95, point.upper_95],
      });
    }
  }
  return rows;
}

// 通知履歴があるポイントは zスコアとAIレポート抜粋を、無いポイントは終値のみを表示する。
// デフォルトのTooltipはdataKeyに束ねられた系列しか出せないため、カスタム描画で対応する。
function ChartTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean;
  payload?: { payload: ChartRow }[];
  label?: string;
}) {
  if (!active || !payload || payload.length === 0) return null;
  const row = payload[0].payload;

  return (
    <div className="rounded-md border bg-background p-2 text-sm shadow-sm">
      <p className="font-medium">{label}</p>
      {row.close !== undefined && <p>終値: {row.close.toLocaleString()}</p>}
      {row.notification && (
        <>
          <p>Zスコア: {row.notification.anomaly_score.toFixed(2)}</p>
          <p className="max-w-64 text-muted-foreground">
            {row.notification.ai_report.slice(0, 80)}
            {row.notification.ai_report.length > 80 ? "..." : ""}
          </p>
        </>
      )}
    </div>
  );
}

export function StockChart({ data }: { data: StockChartData }) {
  const rows = buildRows(data);
  const markerRows = rows.filter(
    (r) => r.notification !== undefined && r.close !== undefined,
  );

  return (
    <div className="space-y-2">
      {!data.forecast && (
        <p className="text-sm text-muted-foreground" role="status">
          予測を取得できませんでした
        </p>
      )}
      <ResponsiveContainer width="100%" height={400}>
        <ComposedChart data={rows}>
          <XAxis dataKey="date" />
          <YAxis domain={["auto", "auto"]} />
          <Tooltip content={<ChartTooltip />} />
          <Legend />
          <Area
            dataKey="alertRange"
            stroke="none"
            fill="#f59e0b"
            fillOpacity={0.15}
            name="アラート境界"
            isAnimationActive={false}
          />
          <Area
            dataKey="forecast95Range"
            stroke="none"
            fill="#6366f1"
            fillOpacity={0.08}
            name="統計的期待レンジ（95%）"
            isAnimationActive={false}
          />
          <Area
            dataKey="forecast68Range"
            stroke="none"
            fill="#6366f1"
            fillOpacity={0.15}
            name="統計的期待レンジ（68%）"
            isAnimationActive={false}
          />
          <Line
            dataKey="close"
            stroke="#0f172a"
            dot={false}
            name="終値"
            isAnimationActive={false}
          />
          <Line
            dataKey="forecastCenter"
            stroke="#6366f1"
            strokeDasharray="4 4"
            dot={false}
            name="予測中心線"
            isAnimationActive={false}
          />
          {markerRows.map((r) => (
            <ReferenceDot
              key={r.date}
              x={r.date}
              y={r.close}
              r={5}
              fill="#ef4444"
              stroke="none"
            />
          ))}
        </ComposedChart>
      </ResponsiveContainer>
      <p className="text-xs text-muted-foreground">
        統計的期待レンジは直近120営業日の対数リターンの平均と標準偏差から算出した
        ドリフト＋ボラティリティ区間であり、価格予測ではありません。アラート境界は
        直近30日の平均±アラート閾値σ（実際に通知が発火する境界）を示します。
      </p>
    </div>
  );
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd frontend && npm test -- stock-chart`
Expected: PASS（全件）

- [ ] **Step 5: コミット**

```bash
cd frontend
git add components/stock-chart.tsx components/stock-chart.test.tsx
git commit -m "feat: add StockChart component with alert band, forecast bands, and anomaly markers"
```

---

### Task 9: Frontend — `/stocks/[code]` ページ

**Files:**
- Create: `frontend/app/stocks/[code]/page.tsx`

**Interfaces:**
- Consumes: Task 7の `fetchStockChart`、Task 8の `<StockChart />`、既存の `requireToken`（`frontend/lib/auth.ts`）
- Produces: `/stocks/[code]` ルート。Task 10がここへリンクする。

- [ ] **Step 1: Next.js 16のダイナミックルート規約を確認する**

Run: `cat frontend/node_modules/next/dist/docs/01-app/03-api-reference/03-file-conventions/dynamic-routes.md | head -40`
Expected: `params` が `Promise<{ code: string }>` であり `await params` で受け取る必要があることを確認（既に本プランのGlobal Constraintsで確認済みだが、実装前に再確認する）

- [ ] **Step 2: ページを実装する**

`frontend/app/stocks/[code]/page.tsx` を新規作成:

```tsx
import { notFound, redirect } from "next/navigation";
import { fetchStockChart } from "@/lib/go-api-client";
import { requireToken } from "@/lib/auth";
import { SiteHeader } from "@/components/site-header";
import { StockChart } from "@/components/stock-chart";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";

export default async function StockDetailPage({
  params,
}: {
  params: Promise<{ code: string }>;
}) {
  const { code } = await params;
  const tokenResult = await requireToken();
  if (!tokenResult.ok) {
    redirect("/login");
  }

  const result = await fetchStockChart(tokenResult.token, code);
  if (!result.ok) {
    if (result.status === 401) {
      redirect("/logout");
    }
    if (result.status === 404) {
      notFound();
    }
    throw new Error(result.error);
  }

  const { chart } = result;

  return (
    <div className="flex min-h-dvh flex-col bg-muted/20">
      <SiteHeader />
      <main className="mx-auto w-full max-w-4xl flex-1 space-y-6 p-6 md:p-8">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {chart.stock_code}
          </h1>
          {chart.current_z_score !== null && (
            <p className="text-sm text-muted-foreground">
              現在のZスコア: {chart.current_z_score.toFixed(2)}
            </p>
          )}
        </div>
        <Card>
          <CardHeader>
            <CardTitle>価格と統計的期待レンジ</CardTitle>
          </CardHeader>
          <CardContent>
            <StockChart data={chart} />
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
```

このページはServer Componentかつ外部APIを直接叩く薄いデータ取得層のため、既存の`app/watchlist/page.tsx`と同様にユニットテストは追加しない（`fetchStockChart`はTask 7で、`StockChart`はTask 8でテスト済み）。

- [ ] **Step 3: 型チェックとビルドを確認する**

Run: `cd frontend && npx tsc --noEmit`
Expected: エラーなし

- [ ] **Step 4: コミット**

```bash
cd frontend
git add app/stocks/[code]/page.tsx
git commit -m "feat: add /stocks/[code] chart detail page"
```

---

### Task 10: Frontend — watchlist一覧から詳細ページへのリンク

**Files:**
- Modify: `frontend/components/watchlist-table.tsx`
- Modify: `frontend/components/watchlist-table.test.tsx`

**Interfaces:**
- Consumes: 既存の `WatchlistItem`型、Task 9の `/stocks/[code]` ルート
- Produces: watchlist行の証券コードクリックで `/stocks/{code}` へ遷移するリンク

- [ ] **Step 1: 失敗するテストを書く**

`frontend/components/watchlist-table.test.tsx` の該当テスト（証券コード・銘柄名を描画するテスト）の直後に追記:

```tsx
  it("links the stock code to its detail page", () => {
    render(
      <WatchlistTable
        items={[
          {
            id: "1",
            stock_code: "7203",
            stock_name: "Toyota Motor Corporation",
            alert_threshold: 2.5,
          },
        ]}
      />,
    );

    const link = screen.getByRole("link", { name: "7203" });
    expect(link).toHaveAttribute("href", "/stocks/7203");
  });
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd frontend && npm test -- watchlist-table`
Expected: FAIL（証券コードがリンクとして描画されておらず `getByRole("link", ...)` が見つからない）

- [ ] **Step 3: 証券コードをリンク化する**

`frontend/components/watchlist-table.tsx` のimportに `import Link from "next/link";` を追加し、`<TableCell>{item.stock_code}</TableCell>` を以下に置き換える:

```tsx
      <TableCell>
        <Link
          href={`/stocks/${item.stock_code}`}
          className="underline underline-offset-4"
        >
          {item.stock_code}
        </Link>
      </TableCell>
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd frontend && npm test -- watchlist-table`
Expected: PASS（全件）

- [ ] **Step 5: フロントエンドの全テストとビルドを確認する**

Run: `cd frontend && npm test && npx tsc --noEmit`
Expected: PASS、型エラーなし

- [ ] **Step 6: コミット**

```bash
cd frontend
git add components/watchlist-table.tsx components/watchlist-table.test.tsx
git commit -m "feat: link watchlist stock code to its chart detail page"
```

---

## 最終確認

- [ ] `cd python-engine && uv run pytest -v` が全件PASS
- [ ] `cd go-api && go build ./... && go vet ./... && go test -race -short ./...` が全件PASS
- [ ] `cd frontend && npm test && npx tsc --noEmit` が全件PASS
- [ ] ローカルで `docker-compose`等でPostgresを立てられる場合は `DATABASE_URL=... go test -race ./...`（`-short`無し）も実行し、Task 4のリポジトリ統合テストを確認する
- [ ] ブラウザで watchlist → 銘柄詳細ページへの遷移、チャート描画、予測帯・アラート帯・異常検知マーカーの表示を目視確認する
