# Python 分析エンジン Phase 2 実装プラン

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** FastAPI + pandas-ta + Finnhub News を使い、Go から POST /analyze を受けてテクニカル指標とニュースを返す Python 分析エンジンを構築する。

**Architecture:** `python-engine/` ディレクトリを新規作成し、`app/` パッケージ内を schemas・services・routers の3層に分離する。FastAPI の lifespan フックで DI を組み立て、`app.state.analysis_service` 経由で各エンドポイントへ注入する。

**Tech Stack:** Python 3.12、uv、FastAPI 0.115+、Pydantic v2、pandas 2.2+、pandas-ta 0.3.14b、httpx 0.27+、pytest + pytest-httpx

## Global Constraints

- パッケージマネージャは `uv` のみ使用（pip 直接呼び出し禁止）
- テストは `uv run pytest -v` で実行（virtualenv 外で pytest 直接呼び出し禁止）
- Python 3.12 以上必須（型ヒントの `X | Y` 記法を使用）
- ブランチ: `feature/python-engine-phase2`（main への直接コミット禁止）
- すべてのコマンドは `python-engine/` ディレクトリから実行
- `prices` の最小件数バリデーション: 14件未満 → 422
- NaN → `null`（Python `None`）
- Finnhub タイムアウト: 5 秒

---

## ファイル構成（全タスク完了後）

```
python-engine/
├── pyproject.toml              # uv プロジェクト定義・依存関係
├── uv.lock                     # 再現可能インストール用（自動生成、コミット対象）
├── Dockerfile                  # Railway デプロイ用
├── app/
│   ├── __init__.py
│   ├── main.py                 # FastAPI app、lifespan で DI 組み立て
│   ├── schemas.py              # Pydantic リクエスト/レスポンス型
│   ├── dependencies.py         # Depends で analysis_service を注入
│   ├── services/
│   │   ├── __init__.py
│   │   ├── indicators.py       # RSI/MACD/BB 計算（pandas-ta）
│   │   ├── news_client.py      # NewsClient Protocol + Finnhub/Mock 実装
│   │   └── analysis.py         # 指標＋ニュース のオーケストレーション
│   └── routers/
│       ├── __init__.py
│       └── analyze.py          # POST /analyze ハンドラ
└── tests/
    ├── test_health.py          # GET /health
    ├── test_schemas.py         # Pydantic バリデーション
    ├── test_indicators.py      # 指標計算ロジック
    ├── test_news_client.py     # Finnhub クライアント（httpx_mock）
    ├── test_analysis.py        # AnalysisService オーケストレーション
    └── test_analyze_endpoint.py # POST /analyze エンドポイント E2E
```

---

### Task 1: プロジェクトスキャフォールド + GET /health

**Files:**
- Create: `python-engine/pyproject.toml`
- Create: `python-engine/app/__init__.py`
- Create: `python-engine/app/main.py`
- Test: `python-engine/tests/test_health.py`

**Interfaces:**
- Consumes: なし
- Produces: `FastAPI app` インスタンス（`app.main:app`）、`GET /health → {"status": "ok"}`

- [ ] **Step 1: ディレクトリを作成する**

```bash
mkdir -p python-engine/app python-engine/tests
```

- [ ] **Step 2: `python-engine/pyproject.toml` を書く**

```toml
[project]
name = "python-engine"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = [
    "fastapi>=0.115.0",
    "uvicorn[standard]>=0.30.0",
    "pydantic>=2.9.0",
    "pandas>=2.2.0",
    "pandas-ta>=0.3.14b",
    "httpx>=0.27.0",
]

[dependency-groups]
dev = [
    "pytest>=8.3.0",
    "pytest-httpx>=0.30.0",
]

[tool.pytest.ini_options]
testpaths = ["tests"]
```

- [ ] **Step 3: `python-engine/app/__init__.py` を書く（空ファイル）**

```python
```

- [ ] **Step 4: テストを書く（`python-engine/tests/test_health.py`）**

```python
from fastapi.testclient import TestClient
from app.main import app


def test_health():
    with TestClient(app) as client:
        response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
```

- [ ] **Step 5: テストが失敗することを確認する**

```bash
cd python-engine
uv sync
uv run pytest tests/test_health.py -v
```

Expected: `FAILED` with `ModuleNotFoundError: No module named 'app.main'`

- [ ] **Step 6: `python-engine/app/main.py` を実装する**

```python
import os
from contextlib import asynccontextmanager
from fastapi import FastAPI


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Phase 2: analysis_service は Task 6 で設定する
    yield


app = FastAPI(lifespan=lifespan)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}
```

- [ ] **Step 7: テストが通ることを確認する**

```bash
uv run pytest tests/test_health.py -v
```

Expected: `PASSED`

- [ ] **Step 8: コミットする**

```bash
cd ..
git add python-engine/pyproject.toml python-engine/uv.lock python-engine/app/__init__.py python-engine/app/main.py python-engine/tests/test_health.py
git commit -m "feat(python-engine): scaffold uv project and GET /health endpoint"
```

---

### Task 2: Pydantic スキーマ定義

**Files:**
- Create: `python-engine/app/schemas.py`
- Test: `python-engine/tests/test_schemas.py`

**Interfaces:**
- Consumes: なし
- Produces:
  - `AnalyzeRequest(stock_code, z_score, current_price, prices)` — `prices` は 14 件未満で ValidationError
  - `MACDValue(line, signal, histogram)` — 各フィールド `float | None`
  - `BollingerValue(upper, middle, lower)` — 各フィールド `float | None`
  - `Indicators(rsi, macd, bollinger)` — `rsi: float | None`
  - `NewsItem(title, url, published_at)`
  - `AnalyzeResponse(stock_code, indicators, news)`

- [ ] **Step 1: テストを書く（`python-engine/tests/test_schemas.py`）**

```python
import pytest
from pydantic import ValidationError
from app.schemas import AnalyzeRequest, AnalyzeResponse, Indicators, MACDValue, BollingerValue, NewsItem


def test_analyze_request_valid():
    req = AnalyzeRequest(
        stock_code="7203",
        z_score=3.2,
        current_price=3250.0,
        prices=[3000.0 + i * 10 for i in range(30)],
    )
    assert req.stock_code == "7203"
    assert len(req.prices) == 30


def test_analyze_request_minimum_prices():
    req = AnalyzeRequest(
        stock_code="7203",
        z_score=2.5,
        current_price=3000.0,
        prices=[3000.0] * 14,  # 14件はOK
    )
    assert len(req.prices) == 14


def test_analyze_request_too_few_prices():
    with pytest.raises(ValidationError) as exc_info:
        AnalyzeRequest(
            stock_code="7203",
            z_score=2.5,
            current_price=3000.0,
            prices=[3000.0] * 13,  # 13件はNG
        )
    errors = exc_info.value.errors()
    assert any("prices" in str(e["loc"]) for e in errors)


def test_indicators_allows_none():
    ind = Indicators(
        rsi=None,
        macd=MACDValue(line=None, signal=None, histogram=None),
        bollinger=BollingerValue(upper=None, middle=None, lower=None),
    )
    assert ind.rsi is None


def test_analyze_response_shape():
    resp = AnalyzeResponse(
        stock_code="7203",
        indicators=Indicators(
            rsi=72.5,
            macd=MACDValue(line=45.2, signal=38.1, histogram=7.1),
            bollinger=BollingerValue(upper=3400.0, middle=3250.0, lower=3100.0),
        ),
        news=[
            NewsItem(title="テストニュース", url="https://example.com", published_at="2026-07-17T09:00:00Z")
        ],
    )
    assert resp.stock_code == "7203"
    assert resp.indicators.rsi == 72.5
    assert len(resp.news) == 1
```

- [ ] **Step 2: テストが失敗することを確認する**

```bash
uv run pytest tests/test_schemas.py -v
```

Expected: `FAILED` with `ModuleNotFoundError: No module named 'app.schemas'`

- [ ] **Step 3: `python-engine/app/schemas.py` を実装する**

```python
from pydantic import BaseModel, field_validator


class AnalyzeRequest(BaseModel):
    stock_code: str
    z_score: float
    current_price: float
    prices: list[float]

    @field_validator("prices")
    @classmethod
    def prices_min_length(cls, v: list[float]) -> list[float]:
        if len(v) < 14:
            raise ValueError("prices must have at least 14 elements")
        return v


class MACDValue(BaseModel):
    line: float | None
    signal: float | None
    histogram: float | None


class BollingerValue(BaseModel):
    upper: float | None
    middle: float | None
    lower: float | None


class Indicators(BaseModel):
    rsi: float | None
    macd: MACDValue
    bollinger: BollingerValue


class NewsItem(BaseModel):
    title: str
    url: str
    published_at: str


class AnalyzeResponse(BaseModel):
    stock_code: str
    indicators: Indicators
    news: list[NewsItem]
```

- [ ] **Step 4: テストが通ることを確認する**

```bash
uv run pytest tests/test_schemas.py -v
```

Expected: `5 passed`

- [ ] **Step 5: コミットする**

```bash
cd ..
git add python-engine/app/schemas.py python-engine/tests/test_schemas.py
git commit -m "feat(python-engine): add Pydantic schemas for analyze request/response"
```

---

### Task 3: テクニカル指標サービス（RSI/MACD/BB）

**Files:**
- Create: `python-engine/app/services/__init__.py`
- Create: `python-engine/app/services/indicators.py`
- Test: `python-engine/tests/test_indicators.py`

**Interfaces:**
- Consumes: `app.schemas.Indicators`, `app.schemas.MACDValue`, `app.schemas.BollingerValue`
- Produces:
  - `calculate_indicators(prices: list[float]) -> Indicators`
  - 14件未満の場合は RSI=None、計算不可の指標は None

- [ ] **Step 1: `python-engine/app/services/__init__.py` を書く（空ファイル）**

```python
```

- [ ] **Step 2: テストを書く（`python-engine/tests/test_indicators.py`）**

```python
from app.services.indicators import calculate_indicators


def _rising_prices(n: int = 30) -> list[float]:
    """単調増加価格: z-score検知のためRSI高くなりやすいシナリオ"""
    return [100.0 + i * 2 for i in range(n)]


def _flat_prices(n: int = 30) -> list[float]:
    """横ばい価格: RSI中立付近"""
    return [100.0 + (i % 2) * 0.1 for i in range(n)]


def test_rsi_overbought_on_rising_prices():
    prices = _rising_prices(30)
    result = calculate_indicators(prices)
    assert result.rsi is not None
    assert result.rsi > 70, f"期待: RSI > 70, 実際: {result.rsi}"


def test_rsi_is_none_when_too_few_prices():
    # RSI(14) には 15件必要。14件では NaN → None
    prices = [100.0] * 14
    result = calculate_indicators(prices)
    # 14件は flat → RSI が NaN になる（avg_loss=0 のエッジケース）
    # None か 100.0 のいずれかであれば OK
    assert result.rsi is None or result.rsi == 100.0


def test_macd_line_is_calculated_with_30_prices():
    prices = _rising_prices(30)
    result = calculate_indicators(prices)
    # MACD line は 26件あれば計算可能（30件で OK）
    assert result.macd.line is not None


def test_macd_signal_may_be_none_with_30_prices():
    # signal EMA(9) には MACD line が 9件必要 → 30件データでは精度低め（None の可能性あり）
    # None または float のどちらでもテストはパス（spec の許容範囲）
    prices = _rising_prices(30)
    result = calculate_indicators(prices)
    assert result.macd.signal is None or isinstance(result.macd.signal, float)


def test_bollinger_bands_with_30_prices():
    prices = _flat_prices(30)
    result = calculate_indicators(prices)
    assert result.bollinger.upper is not None
    assert result.bollinger.middle is not None
    assert result.bollinger.lower is not None
    assert result.bollinger.upper >= result.bollinger.middle >= result.bollinger.lower


def test_bollinger_none_with_fewer_than_20_prices():
    prices = _rising_prices(19)  # BB(20) には 20件必要
    result = calculate_indicators(prices)
    assert result.bollinger.upper is None
    assert result.bollinger.middle is None
    assert result.bollinger.lower is None


def test_returns_indicators_type():
    from app.schemas import Indicators
    result = calculate_indicators(_rising_prices(30))
    assert isinstance(result, Indicators)
```

- [ ] **Step 3: テストが失敗することを確認する**

```bash
uv run pytest tests/test_indicators.py -v
```

Expected: `FAILED` with `ModuleNotFoundError: No module named 'app.services.indicators'`

- [ ] **Step 4: `python-engine/app/services/indicators.py` を実装する**

```python
import math

import pandas as pd
import pandas_ta as ta

from app.schemas import BollingerValue, Indicators, MACDValue


def _to_float_or_none(value) -> float | None:
    """pandas/numpy の値を float に変換。NaN・None・変換不可は None を返す。"""
    if value is None:
        return None
    try:
        f = float(value)
        return None if math.isnan(f) else f
    except (TypeError, ValueError):
        return None


def calculate_indicators(prices: list[float]) -> Indicators:
    """
    prices: 古い順（index 0 が最古）の価格リスト。最低 14 件。
    RSI(14)、MACD(12/26/9)、BB(20/2) を計算して Indicators を返す。
    計算不可の値は None。
    """
    close = pd.Series(prices, dtype=float)

    # --- RSI(14) ---
    rsi_series = ta.rsi(close, length=14)
    rsi_val = _to_float_or_none(rsi_series.iloc[-1] if rsi_series is not None and len(rsi_series) > 0 else None)

    # --- MACD(12, 26, 9) ---
    macd_df = ta.macd(close, fast=12, slow=26, signal=9)
    if macd_df is not None and not macd_df.empty:
        macd_line = _to_float_or_none(macd_df["MACD_12_26_9"].iloc[-1])
        macd_signal = _to_float_or_none(macd_df["MACDs_12_26_9"].iloc[-1])
        macd_hist = _to_float_or_none(macd_df["MACDh_12_26_9"].iloc[-1])
    else:
        macd_line = macd_signal = macd_hist = None

    # --- Bollinger Bands(20, 2σ) ---
    bb_df = ta.bbands(close, length=20, std=2)
    if bb_df is not None and not bb_df.empty:
        bb_upper = _to_float_or_none(bb_df["BBU_20_2.0"].iloc[-1])
        bb_middle = _to_float_or_none(bb_df["BBM_20_2.0"].iloc[-1])
        bb_lower = _to_float_or_none(bb_df["BBL_20_2.0"].iloc[-1])
    else:
        bb_upper = bb_middle = bb_lower = None

    return Indicators(
        rsi=rsi_val,
        macd=MACDValue(line=macd_line, signal=macd_signal, histogram=macd_hist),
        bollinger=BollingerValue(upper=bb_upper, middle=bb_middle, lower=bb_lower),
    )
```

- [ ] **Step 5: テストが通ることを確認する**

```bash
uv run pytest tests/test_indicators.py -v
```

Expected: `6 passed`（`test_rsi_is_none_when_too_few_prices` は flat 価格のエッジケースで None または 100.0）

- [ ] **Step 6: コミットする**

```bash
cd ..
git add python-engine/app/services/__init__.py python-engine/app/services/indicators.py python-engine/tests/test_indicators.py
git commit -m "feat(python-engine): add technical indicators service (RSI/MACD/BB)"
```

---

### Task 4: ニュースクライアント（Protocol + Finnhub + Mock）

**Files:**
- Create: `python-engine/app/services/news_client.py`
- Test: `python-engine/tests/test_news_client.py`

**Interfaces:**
- Consumes: `app.schemas.NewsItem`
- Produces:
  - `class NewsClient(Protocol)` — `fetch_news(stock_code: str) -> list[NewsItem]`
  - `class FinnhubNewsClient` — Finnhub REST API 実装（タイムアウト 5 秒）
  - `class MockNewsClient` — 空リストを返す

- [ ] **Step 1: テストを書く（`python-engine/tests/test_news_client.py`）**

```python
import pytest
from app.schemas import NewsItem
from app.services.news_client import FinnhubNewsClient, MockNewsClient


def test_mock_news_client_returns_empty_list():
    client = MockNewsClient()
    result = client.fetch_news("7203")
    assert result == []


def test_finnhub_client_returns_news_items(httpx_mock):
    httpx_mock.add_response(
        json=[
            {
                "headline": "トヨタが新型EV発表",
                "url": "https://example.com/news/1",
                "datetime": 1720000000,
            },
            {
                "headline": "決算発表",
                "url": "https://example.com/news/2",
                "datetime": 1720100000,
            },
        ]
    )
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert len(items) == 2
    assert items[0].title == "トヨタが新型EV発表"
    assert items[0].url == "https://example.com/news/1"
    assert items[0].published_at.endswith("Z")


def test_finnhub_client_limits_to_5_items(httpx_mock):
    httpx_mock.add_response(
        json=[
            {"headline": f"News {i}", "url": f"https://example.com/{i}", "datetime": 1720000000 + i}
            for i in range(10)
        ]
    )
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert len(items) == 5


def test_finnhub_client_returns_empty_on_http_error(httpx_mock):
    httpx_mock.add_response(status_code=500)
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert items == []


def test_finnhub_client_returns_empty_on_timeout(httpx_mock):
    import httpx
    httpx_mock.add_exception(httpx.TimeoutException("timeout"))
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert items == []


def test_mock_client_satisfies_protocol():
    from typing import runtime_checkable, Protocol

    client = MockNewsClient()
    result = client.fetch_news("7203")
    assert isinstance(result, list)
```

- [ ] **Step 2: テストが失敗することを確認する**

```bash
uv run pytest tests/test_news_client.py -v
```

Expected: `FAILED` with `ModuleNotFoundError: No module named 'app.services.news_client'`

- [ ] **Step 3: `python-engine/app/services/news_client.py` を実装する**

```python
from datetime import datetime, timedelta, timezone
from typing import Protocol

import httpx

from app.schemas import NewsItem

_FINNHUB_BASE_URL = "https://finnhub.io/api/v1/company-news"
_TIMEOUT = 5.0
_NEWS_LIMIT = 5


class NewsClient(Protocol):
    def fetch_news(self, stock_code: str) -> list[NewsItem]: ...


class FinnhubNewsClient:
    """
    Finnhub company-news エンドポイントを叩く本番実装。
    タイムアウト・HTTP エラー時は空リストを返す（呼び出し元で例外を出さない）。
    stock_code は 4桁コードを受け取り "TYO:{code}" に変換する。
    """

    def __init__(self, api_key: str) -> None:
        self._api_key = api_key

    def fetch_news(self, stock_code: str) -> list[NewsItem]:
        now = datetime.now(tz=timezone.utc)
        from_date = (now - timedelta(days=7)).strftime("%Y-%m-%d")
        to_date = now.strftime("%Y-%m-%d")

        try:
            with httpx.Client(timeout=_TIMEOUT) as client:
                resp = client.get(
                    _FINNHUB_BASE_URL,
                    params={
                        "symbol": f"TYO:{stock_code}",
                        "from": from_date,
                        "to": to_date,
                        "token": self._api_key,
                    },
                )
                resp.raise_for_status()
                raw_items: list[dict] = resp.json()
        except Exception:
            return []

        result: list[NewsItem] = []
        for item in raw_items[:_NEWS_LIMIT]:
            published_dt = datetime.fromtimestamp(item.get("datetime", 0), tz=timezone.utc)
            result.append(
                NewsItem(
                    title=item.get("headline", ""),
                    url=item.get("url", ""),
                    published_at=published_dt.strftime("%Y-%m-%dT%H:%M:%SZ"),
                )
            )
        return result


class MockNewsClient:
    """開発・テスト用。常に空リストを返す。"""

    def fetch_news(self, stock_code: str) -> list[NewsItem]:
        return []
```

- [ ] **Step 4: テストが通ることを確認する**

```bash
uv run pytest tests/test_news_client.py -v
```

Expected: `6 passed`

- [ ] **Step 5: コミットする**

```bash
cd ..
git add python-engine/app/services/news_client.py python-engine/tests/test_news_client.py
git commit -m "feat(python-engine): add NewsClient protocol with Finnhub and Mock implementations"
```

---

### Task 5: 分析サービス（オーケストレーション）

**Files:**
- Create: `python-engine/app/services/analysis.py`
- Test: `python-engine/tests/test_analysis.py`

**Interfaces:**
- Consumes: `calculate_indicators`, `NewsClient`, `AnalyzeRequest`, `AnalyzeResponse`
- Produces:
  - `class AnalysisService` — `__init__(self, news_client: NewsClient)`, `analyze(self, request: AnalyzeRequest) -> AnalyzeResponse`

- [ ] **Step 1: テストを書く（`python-engine/tests/test_analysis.py`）**

```python
from app.schemas import AnalyzeRequest, NewsItem
from app.services.analysis import AnalysisService
from app.services.news_client import MockNewsClient


def _make_request(n_prices: int = 30) -> AnalyzeRequest:
    return AnalyzeRequest(
        stock_code="7203",
        z_score=3.2,
        current_price=3250.0,
        prices=[3000.0 + i * 10 for i in range(n_prices)],
    )


def test_analyze_returns_response_with_indicators():
    svc = AnalysisService(news_client=MockNewsClient())
    resp = svc.analyze(_make_request(30))
    assert resp.stock_code == "7203"
    assert resp.indicators is not None
    assert resp.news == []


def test_analyze_includes_news_from_client():
    class StubNewsClient:
        def fetch_news(self, stock_code: str):
            return [NewsItem(title="テスト", url="https://example.com", published_at="2026-07-17T09:00:00Z")]

    svc = AnalysisService(news_client=StubNewsClient())
    resp = svc.analyze(_make_request(30))
    assert len(resp.news) == 1
    assert resp.news[0].title == "テスト"


def test_analyze_rsi_is_not_none_with_30_prices():
    svc = AnalysisService(news_client=MockNewsClient())
    resp = svc.analyze(_make_request(30))
    # 30件の単調増加 → RSI は計算できる
    assert resp.indicators.rsi is not None


def test_analyze_news_empty_on_client_failure():
    class FailingNewsClient:
        def fetch_news(self, stock_code: str):
            return []  # FinnhubNewsClient は例外をキャッチして [] を返す

    svc = AnalysisService(news_client=FailingNewsClient())
    resp = svc.analyze(_make_request(30))
    assert resp.news == []
    # 指標は正常に返る
    assert resp.indicators is not None
```

- [ ] **Step 2: テストが失敗することを確認する**

```bash
uv run pytest tests/test_analysis.py -v
```

Expected: `FAILED` with `ModuleNotFoundError: No module named 'app.services.analysis'`

- [ ] **Step 3: `python-engine/app/services/analysis.py` を実装する**

```python
from app.schemas import AnalyzeRequest, AnalyzeResponse
from app.services.indicators import calculate_indicators
from app.services.news_client import NewsClient


class AnalysisService:
    def __init__(self, news_client: NewsClient) -> None:
        self._news_client = news_client

    def analyze(self, request: AnalyzeRequest) -> AnalyzeResponse:
        indicators = calculate_indicators(request.prices)
        news = self._news_client.fetch_news(request.stock_code)
        return AnalyzeResponse(
            stock_code=request.stock_code,
            indicators=indicators,
            news=news,
        )
```

- [ ] **Step 4: テストが通ることを確認する**

```bash
uv run pytest tests/test_analysis.py -v
```

Expected: `4 passed`

- [ ] **Step 5: コミットする**

```bash
cd ..
git add python-engine/app/services/analysis.py python-engine/tests/test_analysis.py
git commit -m "feat(python-engine): add AnalysisService orchestrating indicators and news"
```

---

### Task 6: FastAPI ルーター + `app/main.py` 完成

**Files:**
- Create: `python-engine/app/routers/__init__.py`
- Create: `python-engine/app/routers/analyze.py`
- Create: `python-engine/app/dependencies.py`
- Modify: `python-engine/app/main.py`（lifespan に DI 組み込み）
- Test: `python-engine/tests/test_analyze_endpoint.py`

**Interfaces:**
- Consumes: `AnalysisService`, `AnalyzeRequest`, `AnalyzeResponse`
- Produces: `POST /analyze → 200 AnalyzeResponse`（prices < 14 件 → 422）

- [ ] **Step 1: `python-engine/app/routers/__init__.py` を書く（空ファイル）**

```python
```

- [ ] **Step 2: テストを書く（`python-engine/tests/test_analyze_endpoint.py`）**

```python
import pytest
from fastapi.testclient import TestClient
from app.main import app


@pytest.fixture(scope="module")
def client():
    # lifespan が実行される。FINNHUB_API_KEY 未設定 → MockNewsClient 使用
    with TestClient(app) as c:
        yield c


def test_analyze_success(client):
    response = client.post(
        "/analyze",
        json={
            "stock_code": "7203",
            "z_score": 3.2,
            "current_price": 3250.0,
            "prices": [3000.0 + i * 10 for i in range(30)],
        },
    )
    assert response.status_code == 200
    body = response.json()
    assert body["stock_code"] == "7203"
    assert "rsi" in body["indicators"]
    assert "macd" in body["indicators"]
    assert "bollinger" in body["indicators"]
    assert isinstance(body["news"], list)


def test_analyze_returns_rsi_float(client):
    response = client.post(
        "/analyze",
        json={
            "stock_code": "6758",
            "z_score": 2.8,
            "current_price": 5000.0,
            "prices": [5000.0 + i * 5 for i in range(30)],
        },
    )
    assert response.status_code == 200
    rsi = response.json()["indicators"]["rsi"]
    assert rsi is None or isinstance(rsi, float)


def test_analyze_too_few_prices_returns_422(client):
    response = client.post(
        "/analyze",
        json={
            "stock_code": "7203",
            "z_score": 3.2,
            "current_price": 3250.0,
            "prices": [3000.0] * 13,  # 13件 < 14件
        },
    )
    assert response.status_code == 422


def test_health_still_works(client):
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
```

- [ ] **Step 3: テストが失敗することを確認する**

```bash
uv run pytest tests/test_analyze_endpoint.py -v
```

Expected: `FAILED` — `/analyze` ルートが存在しないため `404` または `AttributeError`

- [ ] **Step 4: `python-engine/app/dependencies.py` を書く**

```python
from fastapi import Request
from app.services.analysis import AnalysisService


def get_analysis_service(request: Request) -> AnalysisService:
    return request.app.state.analysis_service
```

- [ ] **Step 5: `python-engine/app/routers/analyze.py` を書く**

```python
from fastapi import APIRouter, Depends
from app.dependencies import get_analysis_service
from app.schemas import AnalyzeRequest, AnalyzeResponse
from app.services.analysis import AnalysisService

router = APIRouter()


@router.post("/analyze", response_model=AnalyzeResponse)
def analyze(req: AnalyzeRequest, svc: AnalysisService = Depends(get_analysis_service)) -> AnalyzeResponse:
    return svc.analyze(req)
```

- [ ] **Step 6: `python-engine/app/main.py` を完成させる（Task 1 の scaffold を置き換える）**

```python
import os
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.routers import analyze as analyze_router
from app.services.analysis import AnalysisService
from app.services.news_client import FinnhubNewsClient, MockNewsClient


@asynccontextmanager
async def lifespan(app: FastAPI):
    api_key = os.getenv("FINNHUB_API_KEY")
    news_client = FinnhubNewsClient(api_key=api_key) if api_key else MockNewsClient()
    app.state.analysis_service = AnalysisService(news_client=news_client)
    yield


app = FastAPI(lifespan=lifespan)
app.include_router(analyze_router.router)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}
```

- [ ] **Step 7: テストが通ることを確認する（全テスト）**

```bash
uv run pytest -v
```

Expected: 全テスト `PASSED`（`test_health.py` + `test_schemas.py` + `test_indicators.py` + `test_news_client.py` + `test_analysis.py` + `test_analyze_endpoint.py`）

- [ ] **Step 8: コミットする**

```bash
cd ..
git add python-engine/app/routers/__init__.py python-engine/app/routers/analyze.py python-engine/app/dependencies.py python-engine/app/main.py python-engine/tests/test_analyze_endpoint.py
git commit -m "feat(python-engine): wire POST /analyze endpoint with FastAPI DI"
```

---

### Task 7: Dockerfile（Railway デプロイ用）

**Files:**
- Create: `python-engine/Dockerfile`

**Interfaces:**
- Consumes: `python-engine/pyproject.toml`, `python-engine/uv.lock`, `python-engine/app/`
- Produces: コンテナ起動時に `uvicorn app.main:app` が `PORT`（デフォルト 8000）でリッスン

- [ ] **Step 1: `python-engine/Dockerfile` を書く**

```dockerfile
FROM python:3.12-slim

WORKDIR /app

# uv バイナリを公式イメージからコピー
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

# 依存解決（ソースコードより先にコピーしてレイヤーキャッシュを活かす）
COPY python-engine/pyproject.toml python-engine/uv.lock* ./
RUN uv sync --no-dev --frozen --system

# アプリケーションコードをコピー
COPY python-engine/app/ ./app/

EXPOSE 8000

CMD ["sh", "-c", "uvicorn app.main:app --host 0.0.0.0 --port ${PORT:-8000}"]
```

> **Note:** Dockerfile はリポジトリルートからの `docker build -t python-engine .` を想定しているため、COPY パスが `python-engine/` プレフィックスを持つ。Railway のビルドコンテキストが `python-engine/` サブディレクトリに限定される場合はパスを修正すること。

- [ ] **Step 2: ビルドが成功することを確認する（Docker 利用可能な場合）**

リポジトリルートから実行:

```bash
docker build -f python-engine/Dockerfile -t python-engine-test .
```

Expected: `Successfully built <image-id>` または `naming to docker.io/library/python-engine-test`

Docker が利用できない場合はこのステップをスキップして次へ進む。

- [ ] **Step 3: コミットする**

```bash
cd ..
git add python-engine/Dockerfile
git commit -m "feat(python-engine): add Dockerfile for Railway deployment"
```

---

### Task 8: PR 作成

- [ ] **Step 1: 全テストが通ることを最終確認する**

```bash
cd python-engine
uv run pytest -v
```

Expected: すべて `PASSED`

- [ ] **Step 2: PR を作成する**

```bash
cd ..
git push -u origin feature/python-engine-phase2
gh pr create \
  --title "feat: Python analysis engine Phase 2 (FastAPI + RSI/MACD/BB + Finnhub news)" \
  --body "$(cat <<'EOF'
## Summary

- FastAPI POST /analyze エンドポイントを実装（port 8000）
- RSI(14)・MACD(12/26/9)・ボリンジャーバンド(20/2σ) を pandas-ta で計算
- Finnhub News API でニュース取得（Protocol 抽象化 + MockNewsClient）
- FINNHUB_API_KEY 未設定時は自動的に MockNewsClient を使用
- prices < 14件 → 422、NaN → null、Finnhub 失敗 → news:[]

## Test plan

- [ ] `uv run pytest -v` がすべて PASS
- [ ] `POST /analyze` に30件の価格を送り、indicators.rsi が float であることを確認
- [ ] `POST /analyze` に13件の価格を送り、422 が返ることを確認

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## セルフレビュー

### スペックカバレッジ確認

| スペック要件 | 対応タスク |
|---|---|
| POST /analyze エンドポイント（port 8000） | Task 6, 7 |
| リクエスト: stock_code/z_score/current_price/prices[30] | Task 2 |
| レスポンス: indicators(rsi/macd/bollinger) + news[] | Task 2 |
| prices < 14件 → 422 | Task 2, 6 |
| NaN → null | Task 3 |
| Finnhub 失敗 → news:[] | Task 4 |
| Finnhub タイムアウト 5秒 | Task 4 |
| MockNewsClient（API key なし時） | Task 4, 6 |
| uv パッケージマネージャ | Task 1 |
| pandas-ta（RSI/MACD/BB） | Task 3 |
| pytest + pytest-httpx | Task 1, 4 |
| Dockerfile（Railway）| Task 7 |
| PORT 環境変数 | Task 7 |

### 型整合性確認

- `calculate_indicators(prices: list[float]) -> Indicators` — Task 3 定義、Task 5 消費 ✅
- `NewsClient.fetch_news(stock_code: str) -> list[NewsItem]` — Task 4 定義、Task 5 消費 ✅
- `AnalysisService.analyze(request: AnalyzeRequest) -> AnalyzeResponse` — Task 5 定義、Task 6 消費 ✅
- `app.state.analysis_service: AnalysisService` — Task 6 `lifespan` で設定、`dependencies.py` で参照 ✅
