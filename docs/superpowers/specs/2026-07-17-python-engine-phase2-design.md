# Python 分析エンジン Phase 2 設計書

## 概要

Go API サーバーが Z-score 異常を検知した際に呼び出される Python 分析エンジン。
テクニカル指標（RSI/MACD/ボリンジャーバンド）の計算と Finnhub News API によるニュース取得を行い、結果を Go へ返す。

**Phase 2 スコープ**: テクニカル指標計算＋ニュース取得のみ。Claude API・Slack 通知は Phase 3。

---

## API コントラクト

### エンドポイント

`POST /analyze`（ポート 8000）

### リクエスト（Go → Python）

```json
{
  "stock_code": "7203",
  "z_score": 3.2,
  "current_price": 3250.0,
  "prices": [3100.0, 3150.0, "...", 3250.0]
}
```

- `prices`: 古い順（インデックス0が最古）の直近30件。Go の Redis キャッシュから取得した値をそのまま送る。

### レスポンス（Python → Go）

```json
{
  "stock_code": "7203",
  "indicators": {
    "rsi": 72.5,
    "macd": {
      "line": 45.2,
      "signal": 38.1,
      "histogram": 7.1
    },
    "bollinger": {
      "upper": 3400.0,
      "middle": 3250.0,
      "lower": 3100.0
    }
  },
  "news": [
    {
      "title": "トヨタが新型EV発表",
      "url": "https://example.com/news/1",
      "published_at": "2026-07-17T09:00:00Z"
    }
  ]
}
```

- 指標が計算できない場合（NaN）は `null` を返す
- ニュース取得失敗時は `news: []` で継続（指標だけ返す）

---

## テクニカル指標

| 指標 | パラメータ | 必要データ数 | 30件での状況 |
|------|-----------|------------|------------|
| RSI | 14期間 | 15件以上 | ✅ 問題なし |
| MACD | 12/26/9 | 26件以上（signal は35件が理想） | △ MACD線は計算可、signal は精度低め |
| ボリンジャーバンド | 20期間 | 20件以上 | ✅ 問題なし |

30件で計算可能。精度の改善が必要になった場合は Go のキャッシュ件数拡張で対応する（将来対応）。

---

## ディレクトリ構成

```
python-engine/
├── app/
│   ├── main.py              # FastAPI アプリ起動・DI組み立て
│   ├── schemas.py           # Pydantic リクエスト/レスポンス型
│   ├── routers/
│   │   └── analyze.py       # POST /analyze ハンドラ
│   └── services/
│       ├── analysis.py      # 指標＋ニュースのオーケストレーション
│       ├── indicators.py    # RSI/MACD/BB 計算（pandas-ta 使用）
│       └── news_client.py   # Finnhub クライアント（Protocol で抽象化）
├── tests/
│   ├── test_indicators.py   # 指標計算の単体テスト
│   ├── test_news_client.py  # モック Finnhub でニュース取得テスト
│   └── test_analyze.py      # FastAPI TestClient でエンドポイントテスト
├── pyproject.toml           # uv 管理・依存定義
└── Dockerfile               # Railway デプロイ用
```

---

## 依存ライブラリ

| ライブラリ | 用途 |
|-----------|------|
| `fastapi` | HTTP サーバー |
| `uvicorn` | ASGI サーバー |
| `pydantic` | リクエスト/レスポンス型バリデーション |
| `pandas` | 価格データ操作 |
| `pandas-ta` | RSI/MACD/BB 計算 |
| `httpx` | Finnhub HTTP クライアント |
| `pytest` | テスト |
| `pytest-httpx` | httpx モック |

パッケージ管理: **uv**

---

## Finnhub クライアントの抽象化

`NewsClient` を Protocol で定義し、DI で差し替え可能にする。

```python
# services/news_client.py
from typing import Protocol

class NewsClient(Protocol):
    def fetch_news(self, stock_code: str) -> list[NewsItem]: ...

class FinnhubNewsClient:
    """本番実装。FINNHUB_API_KEY 環境変数が必要。"""
    def __init__(self, api_key: str): ...

class MockNewsClient:
    """開発・テスト用。空リストを返す。"""
    def fetch_news(self, stock_code: str) -> list[NewsItem]:
        return []
```

`main.py` で `FINNHUB_API_KEY` 環境変数の有無により自動切り替え:

```python
news_client = (
    FinnhubNewsClient(api_key=os.environ["FINNHUB_API_KEY"])
    if os.getenv("FINNHUB_API_KEY")
    else MockNewsClient()
)
```

---

## エラーハンドリング

| シナリオ | 対応 |
|----------|------|
| `prices` が14件未満 | `422 Unprocessable Entity`（Pydantic バリデーション） |
| 指標計算で NaN が出る | `null` としてレスポンスに含める |
| Finnhub タイムアウト・エラー | `news: []` で継続（Finnhub タイムアウトは 5秒） |
| 予期しない例外 | `500 Internal Server Error` ＋ スタックトレースをログ出力 |

Go 側タイムアウトは 30秒（ADR-004）。

---

## テスト方針

```python
# test_indicators.py — 既知の入力→期待値で検証
def test_rsi_overbought():
    prices = [100.0] * 14 + [130.0]  # 急騰シナリオ
    result = calculate_indicators(prices)
    assert result.rsi > 70

# test_news_client.py — pytest-httpx でモック
def test_fetch_news_returns_items(httpx_mock):
    httpx_mock.add_response(json={"news": [{"headline": "test", ...}]})
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert len(items) == 1

# test_analyze.py — FastAPI TestClient でエンドポイント E2E
def test_analyze_endpoint():
    response = client.post("/analyze", json={
        "stock_code": "7203",
        "z_score": 3.2,
        "current_price": 3250.0,
        "prices": [3000.0 + i for i in range(30)]
    })
    assert response.status_code == 200
    assert "rsi" in response.json()["indicators"]
```

---

## 環境変数

| 変数 | 説明 | デフォルト |
|------|------|-----------|
| `FINNHUB_API_KEY` | Finnhub API キー | 未設定時は MockNewsClient を使用 |
| `PORT` | サーバーポート | `8000` |

---

## 非ゴール（Phase 3 以降）

- Claude API による AI レポート生成
- Slack Webhook 通知
- 通知履歴の PostgreSQL 保存
- Go 側の python_engine_client.go 実装
