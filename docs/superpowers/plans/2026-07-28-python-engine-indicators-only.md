# Python エンジン指標特化リファクタリング 実装プラン

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Python エンジンからニュース取得を除去し、テクニカル指標計算（RSI/MACD/BB）のみに責務を絞る。

**Architecture:** `POST /analyze` は `calculate_indicators(prices)` を直接呼び出し `AnalyzeResponse(stock_code, indicators)` を返す。`AnalysisService`・`NewsClient`・DI レイヤーは不要になるため削除する。`app/main.py` の lifespan も削除してシンプルな `FastAPI()` に戻す。

**Tech Stack:** Python 3.12、uv、FastAPI、Pydantic v2、pandas-ta

## Global Constraints

- パッケージマネージャは `uv` のみ（pip 直接呼び出し禁止）
- テストは `cd python-engine && uv run pytest -v` で実行
- ブランチ: `feature/python-engine-indicators-only`（main への直接コミット禁止）
- git コマンドはリポジトリルート `/Users/watanabekeisuke/Documents/stock-anomaly-detection` から実行
- `prices` 最小 14 件バリデーションは維持（変更なし）
- NaN → `null` は維持（変更なし）

---

## ファイル構成（リファクタリング後）

```
python-engine/
├── pyproject.toml              # pytest-httpx を dev 依存から削除
├── app/
│   ├── __init__.py
│   ├── main.py                 # lifespan・DI 削除、シンプルな FastAPI()
│   ├── schemas.py              # NewsItem・news 削除、AnalyzeResponse を簡素化
│   ├── routers/
│   │   ├── __init__.py
│   │   └── analyze.py          # calculate_indicators() を直接呼び出し
│   └── services/
│       ├── __init__.py
│       └── indicators.py       # 変更なし
│
│   [削除]
│   ├── dependencies.py         # 削除
│   ├── services/analysis.py    # 削除
│   └── services/news_client.py # 削除
│
└── tests/
    ├── test_health.py          # 変更なし
    ├── test_indicators.py      # 変更なし
    ├── test_schemas.py         # NewsItem 関連テスト削除
    └── test_analyze_endpoint.py # news アサーション削除

    [削除]
    ├── test_news_client.py     # 削除
    └── test_analysis.py        # 削除
```

---

### Task 1: ニュース関連コードを全削除・スキーマ・ルーター・main を簡素化

**Files:**
- Delete: `python-engine/app/services/news_client.py`
- Delete: `python-engine/app/services/analysis.py`
- Delete: `python-engine/app/dependencies.py`
- Delete: `python-engine/tests/test_news_client.py`
- Delete: `python-engine/tests/test_analysis.py`
- Modify: `python-engine/app/schemas.py`
- Modify: `python-engine/app/routers/analyze.py`
- Modify: `python-engine/app/main.py`
- Modify: `python-engine/tests/test_schemas.py`
- Modify: `python-engine/tests/test_analyze_endpoint.py`
- Modify: `python-engine/pyproject.toml`

**Interfaces:**
- Consumes: `calculate_indicators(prices: list[float]) -> Indicators`（`app.services.indicators` — 変更なし）
- Produces:
  - `AnalyzeRequest(stock_code, z_score, current_price, prices)` — 変更なし
  - `AnalyzeResponse(stock_code: str, indicators: Indicators)` — `news` フィールド削除

- [ ] **Step 1: テストを先に更新する（テストが現状を反映するように）**

`python-engine/tests/test_schemas.py` を以下の内容に**全て書き換える**:

```python
import pytest
from pydantic import ValidationError
from app.schemas import AnalyzeRequest, AnalyzeResponse, Indicators, MACDValue, BollingerValue


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
        prices=[3000.0] * 14,
    )
    assert len(req.prices) == 14


def test_analyze_request_too_few_prices():
    with pytest.raises(ValidationError) as exc_info:
        AnalyzeRequest(
            stock_code="7203",
            z_score=2.5,
            current_price=3000.0,
            prices=[3000.0] * 13,
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
    )
    assert resp.stock_code == "7203"
    assert resp.indicators.rsi == 72.5
```

`python-engine/tests/test_analyze_endpoint.py` を以下の内容に**全て書き換える**:

```python
import pytest
from fastapi.testclient import TestClient
from app.main import app


@pytest.fixture(scope="module")
def client():
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
    assert "news" not in body


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
            "prices": [3000.0] * 13,
        },
    )
    assert response.status_code == 422


def test_health_still_works(client):
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
```

- [ ] **Step 2: テストが失敗することを確認する**

```bash
cd python-engine
uv run pytest tests/test_schemas.py tests/test_analyze_endpoint.py -v
```

Expected: `FAILED` — `AnalyzeResponse` に `news` が必須フィールドとして残っているためエラーになる。または `NewsItem` が import できないエラー。

- [ ] **Step 3: 不要ファイルを削除する**

```bash
rm python-engine/app/services/news_client.py
rm python-engine/app/services/analysis.py
rm python-engine/app/dependencies.py
rm python-engine/tests/test_news_client.py
rm python-engine/tests/test_analysis.py
```

- [ ] **Step 4: `python-engine/app/schemas.py` を書き換える**

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


class AnalyzeResponse(BaseModel):
    stock_code: str
    indicators: Indicators
```

- [ ] **Step 5: `python-engine/app/routers/analyze.py` を書き換える**

```python
from fastapi import APIRouter
from app.schemas import AnalyzeRequest, AnalyzeResponse
from app.services.indicators import calculate_indicators

router = APIRouter()


@router.post("/analyze", response_model=AnalyzeResponse)
def analyze(req: AnalyzeRequest) -> AnalyzeResponse:
    indicators = calculate_indicators(req.prices)
    return AnalyzeResponse(stock_code=req.stock_code, indicators=indicators)
```

- [ ] **Step 6: `python-engine/app/main.py` を書き換える**

```python
from fastapi import FastAPI
from app.routers import analyze as analyze_router

app = FastAPI()
app.include_router(analyze_router.router)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}
```

- [ ] **Step 7: `python-engine/pyproject.toml` から `pytest-httpx` を削除する**

`[dependency-groups]` セクションを以下に書き換える:

```toml
[dependency-groups]
dev = [
    "pytest>=8.3.0",
    "ruff>=0.6.0",
]
```

その後 uv.lock を更新:

```bash
cd python-engine
uv sync
```

- [ ] **Step 8: テストが通ることを確認する**

```bash
cd python-engine
uv run pytest -v
```

Expected: 残存する全テストが `PASSED`（`test_health.py` + `test_schemas.py` + `test_indicators.py` + `test_analyze_endpoint.py`）。`test_news_client.py` と `test_analysis.py` は削除済みなので実行されない。

合計テスト数は 27 → 約 18 に減る（削除分: `test_news_client.py` 6件 + `test_analysis.py` 4件 + `test_schemas.py` の `test_analyze_response_shape` 1件 変更）。

- [ ] **Step 9: ruff でリントを確認する**

```bash
cd python-engine
uv run ruff check .
```

Expected: `All checks passed!`

- [ ] **Step 10: コミットする**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add python-engine/
git commit -m "refactor(python-engine): remove news fetching, focus on indicators only"
```

---

### Task 2: PR 作成

**Files:** なし（git 操作のみ）

- [ ] **Step 1: 全テストが通ることを最終確認する**

```bash
cd python-engine
uv run pytest -v
uv run ruff check .
```

- [ ] **Step 2: プッシュして PR を作成する**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git push -u origin feature/python-engine-indicators-only
gh pr create \
  --title "refactor(python-engine): remove news fetching, indicators only" \
  --body "$(cat <<'EOF'
## Summary

- Python エンジンの責務をテクニカル指標計算（RSI/MACD/BB）のみに絞る
- `news_client.py`・`analysis.py`・`dependencies.py` を削除
- `AnalyzeResponse` から `news` フィールドを削除
- `app/main.py` の lifespan・DI を削除しシンプルな `FastAPI()` に
- `POST /analyze` が `calculate_indicators()` を直接呼ぶ構成に
- `pytest-httpx` を dev 依存から削除

## Motivation

ニュース取得はデータ収集であり分析ではない。Go 側が全データを集約する設計に変更。

## Test plan

- [ ] `cd python-engine && uv run pytest -v` — 全テスト PASS
- [ ] `POST /analyze` レスポンスに `news` フィールドが含まれないことを確認
- [ ] `uv run ruff check .` — クリア

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## セルフレビュー

### スペックカバレッジ確認

| 要件 | 対応タスク |
|------|-----------|
| `news_client.py` 削除 | Task 1 Step 3 |
| `analysis.py` 削除 | Task 1 Step 3 |
| `dependencies.py` 削除 | Task 1 Step 3 |
| `AnalyzeResponse` から `news` 削除 | Task 1 Step 4 |
| `POST /analyze` が直接 `calculate_indicators` 呼び出し | Task 1 Step 5 |
| `main.py` の lifespan・DI 削除 | Task 1 Step 6 |
| `pytest-httpx` 削除 | Task 1 Step 7 |
| テスト更新（news アサーション削除） | Task 1 Step 1 |
| ruff クリア確認 | Task 1 Step 9 |

### 型整合性確認

- `calculate_indicators(prices: list[float]) -> Indicators` — indicators.py で定義、routers/analyze.py で消費 ✅
- `AnalyzeResponse(stock_code: str, indicators: Indicators)` — schemas.py で定義、routers/analyze.py で生成、テストで確認 ✅
- `"news" not in body` アサーション — AnalyzeResponse に news がないことを E2E で確認 ✅
