# 日足チャート × AI方向分類器（Phase 2） Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 日足の価格系列から算出した特徴量でMLの方向分類器（ロジスティック回帰）を学習し、既存のドリフト予測中心線に対してwalk-forwardバックテストで統計的に有意に優れていると検証できた場合のみ、中心線の方向をこのモデルに差し替える。

**Architecture:** python-engineに特徴量抽出・ラベル生成・walk-forward検証・インメモリのモデル状態を新設し、既存`/forecast`エンドポイントに`direction_model`情報を統合する。go-apiは既存`BackfillPriceHistoryUsecase`と同じパターンで、起動時＋定期バッチでwatchlist全銘柄の価格履歴を集めてpython-engineの新規`/model/train`を呼ぶ。フロントエンドは`adopted=true`の場合のみ既存チャートにバッジを追加表示する。

**Tech Stack:** Python (FastAPI, pandas, pandas-ta, scikit-learn, scipy), Go, Next.js/React/Recharts

**Spec:** `docs/superpowers/specs/2026-09-29-chart-direction-classifier-design.md`

## Global Constraints

- 依存方向: infrastructure/usecase → domain（逆方向禁止）。リポジトリ・外部サービスのインターフェースはdomainパッケージ内に定義する
- Goの変更は `-race` フラグ必須（`go test -race -short ./...`）
- python-engineの新規依存: `scikit-learn`, `scipy`（`pyproject.toml`に追加）
- ラベルホライズンは既存予測と同じ20営業日（`app.schemas.FORECAST_HORIZON`）
- 特徴量の標準化は学習フォールド内のデータのみでfitし、検証フォールドにはそのパラメータを適用する（リーク防止）
- walk-forwardのembargoは20営業日（ホライズンと同じ）
- 採用基準: 対比較検定（厳密二項検定によるMcNemar検定）でp<0.05 かつ モデルのhit_rate > ベースライン（既存ドリフト方向）のhit_rate
- ヒステリシス窓は5再学習サイクル（未採用→採用は5回連続、採用→未採用は5回中過半数不合格）
- 最小学習行数`MIN_TRAINING_ROWS=200`、最小独立検定サンプル数`MIN_INDEPENDENT_SAMPLES=60`を満たさない場合は学習・採用判定をせず`adopted=false`のまま
- モデル状態はpython-engineのインメモリのみ（DB永続化しない）
- go-api側の再学習は起動時1回＋24時間間隔（環境変数`DIRECTION_MODEL_RETRAIN_INTERVAL`、デフォルト`24h`）
- 新規環境変数は`CLAUDE.md`の「環境変数（本番）」節に追記する

## Review Focus

- 新規watchlist登録直後・銘柄数が少ない状態（学習データ不足）で`/model/train`が呼ばれても、クラッシュせず`adopted=false`のまま静かに終わること
- `/forecast`に渡される価格件数が最小要件（121件）ちょうどの境界で、特徴量計算がNaNにならず`predicted_direction`を計算できる／できない場合にNoneへ安全に倒れること
- 銘柄ごとに価格履歴の開始日・長さが異なる状態（新規追加銘柄が混在）で日付ベースのプール・fold分割が壊れないこと
- `/model/train`が学習中に重ねて呼ばれた場合（409）が、go-api側でエラーとしてログに残るだけで他の機能を止めないこと
- `direction_model`フィールドがgo-api・python-engine・フロントエンドの3層で一貫した契約（`adopted=false`時も全フィールドを持つオブジェクトであり`null`ではない）を守り、片方だけ未実装だとフロントが壊れる、という不整合が起きないこと

---

## Task 1: 特徴量・ベースライン方向・ラベル生成（python-engine）

**Files:**
- Create: `python-engine/app/services/direction_features.py`
- Test: `python-engine/tests/test_direction_features.py`
- Modify: `python-engine/pyproject.toml`（本タスクでは変更不要、既存pandas/pandas_taのみ使用）

**Interfaces:**
- Produces:
  - `FEATURE_COLUMNS: list[str]`（`["momentum_5", "momentum_20", "momentum_60", "rsi_14", "macd_hist", "volatility_20"]`）
  - `compute_feature_matrix(prices: list[float]) -> pd.DataFrame`（列=`FEATURE_COLUMNS`、行iはprices[i]時点。ウォームアップ不足はNaN）
  - `compute_baseline_direction(prices: list[float], window: int) -> pd.Series`（1.0=up, 0.0=down, ウォームアップ不足はNaN）
  - `generate_direction_labels(prices: list[float], horizon: int) -> pd.Series`（1.0/0.0、直近horizon件はNaN）

- [ ] **Step 1: 失敗するテストを書く**

```python
# python-engine/tests/test_direction_features.py
import math

import numpy as np
import pandas as pd
import pytest

from app.services.direction_features import (
    FEATURE_COLUMNS,
    compute_baseline_direction,
    compute_feature_matrix,
    generate_direction_labels,
)


def _rising_prices(n: int, daily_return: float = 0.01, start: float = 1000.0) -> list[float]:
    return [start * (1 + daily_return) ** i for i in range(n)]


def test_compute_feature_matrix_has_expected_columns():
    prices = _rising_prices(150)
    features = compute_feature_matrix(prices)
    assert list(features.columns) == FEATURE_COLUMNS
    assert len(features) == len(prices)


def test_compute_feature_matrix_warmup_rows_are_nan():
    prices = _rising_prices(150)
    features = compute_feature_matrix(prices)
    # momentum_60 は60本の過去終値を要するため、先頭60行はNaN
    assert features["momentum_60"].iloc[:60].isna().all()
    assert not features["momentum_60"].iloc[65:].isna().any()


def test_compute_feature_matrix_momentum_sign_matches_trend():
    prices = _rising_prices(150, daily_return=0.01)
    features = compute_feature_matrix(prices)
    assert (features["momentum_5"].dropna() > 0).all()
    assert (features["momentum_20"].dropna() > 0).all()


def test_compute_baseline_direction_up_for_rising_prices():
    prices = _rising_prices(150, daily_return=0.01)
    baseline = compute_baseline_direction(prices, window=120)
    valid = baseline.dropna()
    assert len(valid) > 0
    assert (valid == 1.0).all()


def test_compute_baseline_direction_down_for_falling_prices():
    prices = _rising_prices(150, daily_return=-0.01)
    baseline = compute_baseline_direction(prices, window=120)
    valid = baseline.dropna()
    assert len(valid) > 0
    assert (valid == 0.0).all()


def test_compute_baseline_direction_warmup_is_nan():
    prices = _rising_prices(150)
    baseline = compute_baseline_direction(prices, window=120)
    assert baseline.iloc[:120].isna().all()


def test_generate_direction_labels_matches_manual_calculation():
    prices = [100.0, 101.0, 99.0, 105.0, 95.0, 110.0]
    labels = generate_direction_labels(prices, horizon=2)
    # t=0: prices[2]=99 < prices[0]=100 -> 0.0
    # t=1: prices[3]=105 > prices[1]=101 -> 1.0
    # t=2: prices[4]=95 < prices[2]=99 -> 0.0
    # t=3: prices[5]=110 > prices[3]=105 -> 1.0
    assert labels.iloc[0] == 0.0
    assert labels.iloc[1] == 1.0
    assert labels.iloc[2] == 0.0
    assert labels.iloc[3] == 1.0


def test_generate_direction_labels_last_horizon_rows_are_nan():
    prices = [100.0, 101.0, 99.0, 105.0, 95.0, 110.0]
    labels = generate_direction_labels(prices, horizon=2)
    assert labels.iloc[4:].isna().all()
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd python-engine && uv run pytest tests/test_direction_features.py -v`
Expected: FAIL（`app.services.direction_features` が存在しない）

- [ ] **Step 3: 最小実装を書く**

```python
# python-engine/app/services/direction_features.py
import numpy as np
import pandas as pd
import pandas_ta as ta

_MOMENTUM_WINDOWS = (5, 20, 60)
_RSI_LENGTH = 14
_MACD_FAST, _MACD_SLOW, _MACD_SIGNAL = 12, 26, 9
_VOLATILITY_WINDOW = 20

FEATURE_COLUMNS = [
    "momentum_5",
    "momentum_20",
    "momentum_60",
    "rsi_14",
    "macd_hist",
    "volatility_20",
]


def compute_feature_matrix(prices: list[float]) -> pd.DataFrame:
    """
    prices: 古い順の終値リスト。
    行iはprices[i]時点の特徴量。ウォームアップ不足の行はNaN（呼び出し側でdropnaする）。
    """
    close = pd.Series(prices, dtype=float)

    features = pd.DataFrame(index=close.index)
    for window in _MOMENTUM_WINDOWS:
        features[f"momentum_{window}"] = close / close.shift(window) - 1.0

    features["rsi_14"] = ta.rsi(close, length=_RSI_LENGTH)

    ema_fast = close.ewm(span=_MACD_FAST, adjust=False).mean()
    ema_slow = close.ewm(span=_MACD_SLOW, adjust=False).mean()
    macd_line = ema_fast - ema_slow
    macd_signal = macd_line.ewm(span=_MACD_SIGNAL, adjust=False).mean()
    features["macd_hist"] = macd_line - macd_signal

    log_return = np.log(close / close.shift(1))
    features["volatility_20"] = log_return.rolling(_VOLATILITY_WINDOW).std()

    return features[FEATURE_COLUMNS]


def compute_baseline_direction(prices: list[float], window: int) -> pd.Series:
    """
    既存のforecast.calculate_forecastと同じ定義（直近window営業日の対数リターン平均、
    その符号）で「時点tにおいて中心線が示す方向」を返す。1.0=強気(up)、0.0=弱気(down)。
    windowに満たない先頭はNaN。
    """
    close = pd.Series(prices, dtype=float)
    log_return = np.log(close / close.shift(1))
    mu = log_return.rolling(window).mean()
    direction = (mu > 0).astype(float)
    direction[mu.isna()] = float("nan")
    return direction


def generate_direction_labels(prices: list[float], horizon: int) -> pd.Series:
    """
    時点tのラベル = prices[t+horizon] > prices[t] なら1.0、そうでなければ0.0。
    直近horizon件はラベル未確定のためNaN。
    """
    close = pd.Series(prices, dtype=float)
    future = close.shift(-horizon)
    label = (future > close).astype(float)
    label[future.isna()] = float("nan")
    return label
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `cd python-engine && uv run pytest tests/test_direction_features.py -v`
Expected: PASS（全7件）

- [ ] **Step 5: コミット**

```bash
git add python-engine/app/services/direction_features.py python-engine/tests/test_direction_features.py
git commit -m "$(cat <<'EOF'
feat(python-engine): add direction classifier feature/label extraction

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: 全銘柄プールデータセット構築（python-engine）

**Files:**
- Create: `python-engine/app/services/direction_dataset.py`
- Test: `python-engine/tests/test_direction_dataset.py`

**Interfaces:**
- Consumes: `FEATURE_COLUMNS`, `compute_feature_matrix`, `compute_baseline_direction`, `generate_direction_labels`（Task 1）
- Produces:
  - `StockPriceSeries`（dataclass: `stock_code: str`, `dates: list[str]`, `prices: list[float]`）
  - `build_pooled_dataset(stocks: list[StockPriceSeries], horizon: int) -> pd.DataFrame`（列: `date, stock_code, momentum_5, momentum_20, momentum_60, rsi_14, macd_hist, volatility_20, baseline_direction, label`。有効行のみ、`date`昇順）

- [ ] **Step 1: 失敗するテストを書く**

```python
# python-engine/tests/test_direction_dataset.py
import pytest

from app.services.direction_dataset import StockPriceSeries, build_pooled_dataset
from app.services.direction_features import FEATURE_COLUMNS


def _series(stock_code: str, n: int, start_date: str, daily_return: float = 0.01) -> StockPriceSeries:
    import datetime

    start = datetime.date.fromisoformat(start_date)
    dates = [(start + datetime.timedelta(days=i)).isoformat() for i in range(n)]
    prices = [1000.0 * (1 + daily_return) ** i for i in range(n)]
    return StockPriceSeries(stock_code=stock_code, dates=dates, prices=prices)


def test_build_pooled_dataset_has_expected_columns():
    stocks = [_series("7203", 200, "2024-01-01")]
    dataset = build_pooled_dataset(stocks, horizon=20)
    expected_columns = {"date", "stock_code", *FEATURE_COLUMNS, "baseline_direction", "label"}
    assert expected_columns == set(dataset.columns)


def test_build_pooled_dataset_pools_multiple_stocks():
    stocks = [
        _series("7203", 200, "2024-01-01"),
        _series("6758", 200, "2024-01-01"),
    ]
    dataset = build_pooled_dataset(stocks, horizon=20)
    assert set(dataset["stock_code"].unique()) == {"7203", "6758"}
    # 各銘柄とも同じ有効行数のはず（同じ長さ・同じ開始日のため）
    counts = dataset["stock_code"].value_counts()
    assert counts["7203"] == counts["6758"]
    assert counts["7203"] > 0


def test_build_pooled_dataset_handles_different_start_dates():
    stocks = [
        _series("7203", 200, "2024-01-01"),
        _series("6758", 150, "2024-03-01"),  # 後から追加された銘柄（履歴が短い）
    ]
    dataset = build_pooled_dataset(stocks, horizon=20)
    assert set(dataset["stock_code"].unique()) == {"7203", "6758"}
    assert dataset["date"].is_monotonic_increasing


def test_build_pooled_dataset_rejects_mismatched_lengths():
    bad = StockPriceSeries(stock_code="7203", dates=["2024-01-01"], prices=[100.0, 101.0])
    with pytest.raises(ValueError):
        build_pooled_dataset([bad], horizon=20)


def test_build_pooled_dataset_empty_input_returns_empty_frame():
    dataset = build_pooled_dataset([], horizon=20)
    assert len(dataset) == 0
    expected_columns = {"date", "stock_code", *FEATURE_COLUMNS, "baseline_direction", "label"}
    assert expected_columns == set(dataset.columns)
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd python-engine && uv run pytest tests/test_direction_dataset.py -v`
Expected: FAIL（`app.services.direction_dataset` が存在しない）

- [ ] **Step 3: 最小実装を書く**

```python
# python-engine/app/services/direction_dataset.py
from dataclasses import dataclass

import pandas as pd

from app.schemas import ESTIMATION_WINDOW
from app.services.direction_features import (
    FEATURE_COLUMNS,
    compute_baseline_direction,
    compute_feature_matrix,
    generate_direction_labels,
)


@dataclass
class StockPriceSeries:
    stock_code: str
    dates: list[str]  # 古い順、pricesと同じ長さ
    prices: list[float]


def build_pooled_dataset(stocks: list[StockPriceSeries], horizon: int) -> pd.DataFrame:
    """
    各銘柄について特徴量・ベースライン方向・ラベルを計算し、日付をまたいで1つの
    DataFrameにプールする。ウォームアップ不足・ラベル未確定の行は自動的に除外される。
    """
    columns = ["date", "stock_code", *FEATURE_COLUMNS, "baseline_direction", "label"]
    frames = []
    for series in stocks:
        if len(series.prices) != len(series.dates):
            raise ValueError(f"{series.stock_code}: dates and prices length mismatch")

        df = compute_feature_matrix(series.prices)
        df["date"] = series.dates
        df["stock_code"] = series.stock_code
        df["baseline_direction"] = compute_baseline_direction(series.prices, ESTIMATION_WINDOW).values
        df["label"] = generate_direction_labels(series.prices, horizon).values
        frames.append(df.dropna()[columns])

    if not frames:
        return pd.DataFrame(columns=columns)

    return pd.concat(frames, ignore_index=True).sort_values("date").reset_index(drop=True)
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `cd python-engine && uv run pytest tests/test_direction_dataset.py -v`
Expected: PASS（全5件）

- [ ] **Step 5: コミット**

```bash
git add python-engine/app/services/direction_dataset.py python-engine/tests/test_direction_dataset.py
git commit -m "$(cat <<'EOF'
feat(python-engine): pool per-stock features into a joint training dataset

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: walk-forward検証ロジック（fold分割・非重複サンプリング・対比較検定・ヒステリシス）

**Files:**
- Create: `python-engine/app/services/walk_forward.py`
- Create: `python-engine/app/services/adoption_state.py`
- Test: `python-engine/tests/test_walk_forward.py`
- Test: `python-engine/tests/test_adoption_state.py`
- Modify: `python-engine/pyproject.toml`（`scipy`を追加）

**Interfaces:**
- Produces:
  - `WalkForwardFold`（frozen dataclass: `train_dates: list[str]`, `test_dates: list[str]`）
  - `make_walk_forward_folds(sorted_unique_dates: list[str], n_folds: int, embargo: int) -> list[WalkForwardFold]`
  - `non_overlapping_sample(sorted_dates: list[str], step: int) -> list[str]`
  - `paired_significance_test(model_correct: np.ndarray, baseline_correct: np.ndarray) -> float`（p値を返す）
  - `AdoptionState`（frozen dataclass: `recent_results: tuple[bool, ...]`, `adopted: bool`）
  - `update_adoption_state(state: AdoptionState, meets_criteria: bool, window: int) -> AdoptionState`

- [ ] **Step 1: 失敗するテストを書く**

```python
# python-engine/tests/test_walk_forward.py
import numpy as np

from app.services.walk_forward import (
    make_walk_forward_folds,
    non_overlapping_sample,
    paired_significance_test,
)


def _dates(n: int) -> list[str]:
    import datetime

    start = datetime.date(2024, 1, 1)
    return [(start + datetime.timedelta(days=i)).isoformat() for i in range(n)]


def test_make_walk_forward_folds_respects_embargo():
    dates = _dates(300)
    folds = make_walk_forward_folds(dates, n_folds=5, embargo=20)
    assert len(folds) > 0
    for fold in folds:
        train_end_date = fold.train_dates[-1]
        test_start_date = fold.test_dates[0]
        train_end_idx = dates.index(train_end_date)
        test_start_idx = dates.index(test_start_date)
        assert test_start_idx - train_end_idx >= 20


def test_make_walk_forward_folds_excludes_folds_with_too_little_training_data():
    dates = _dates(300)
    folds = make_walk_forward_folds(dates, n_folds=5, embargo=20)
    for fold in folds:
        assert len(fold.train_dates) >= 20 * 3


def test_make_walk_forward_folds_empty_when_too_few_dates():
    dates = _dates(10)
    folds = make_walk_forward_folds(dates, n_folds=5, embargo=20)
    assert folds == []


def test_non_overlapping_sample_steps_by_horizon():
    dates = _dates(100)
    sampled = non_overlapping_sample(dates, step=20)
    assert sampled == dates[::20]
    assert len(sampled) == 5


def test_paired_significance_test_significant_when_model_much_better():
    rng = np.random.default_rng(0)
    n = 200
    baseline_correct = rng.random(n) < 0.5
    # モデルは常にbaselineが外した分も含めて的中する、という極端なケース
    model_correct = np.ones(n, dtype=bool)
    p = paired_significance_test(model_correct, baseline_correct)
    assert p < 0.05


def test_paired_significance_test_not_significant_when_equal():
    rng = np.random.default_rng(1)
    n = 200
    model_correct = rng.random(n) < 0.5
    baseline_correct = model_correct.copy()
    p = paired_significance_test(model_correct, baseline_correct)
    assert p == 1.0  # 完全に一致 = 食い違いゼロ


def test_paired_significance_test_no_disagreement_returns_one():
    model_correct = np.array([True, True, False, False])
    baseline_correct = np.array([True, True, False, False])
    p = paired_significance_test(model_correct, baseline_correct)
    assert p == 1.0
```

```python
# python-engine/tests/test_adoption_state.py
from app.services.adoption_state import AdoptionState, update_adoption_state


def test_adopts_after_consecutive_successes():
    state = AdoptionState()
    for _ in range(4):
        state = update_adoption_state(state, True, window=5)
        assert state.adopted is False
    state = update_adoption_state(state, True, window=5)
    assert state.adopted is True


def test_does_not_adopt_if_streak_broken():
    state = AdoptionState()
    for _ in range(4):
        state = update_adoption_state(state, True, window=5)
    state = update_adoption_state(state, False, window=5)
    assert state.adopted is False
    # 直後にまた4回成功しても、直近5回の窓に失敗が1回含まれる限り採用されない
    for _ in range(4):
        state = update_adoption_state(state, True, window=5)
    assert state.adopted is False


def test_stays_adopted_unless_majority_fail():
    state = AdoptionState(recent_results=(True, True, True, True, True), adopted=True)
    # 直近5回中2回失敗 -> 過半数ではないので採用維持
    state = update_adoption_state(state, False, window=5)
    state = update_adoption_state(state, False, window=5)
    assert state.adopted is True


def test_reverts_when_majority_fail():
    state = AdoptionState(recent_results=(True, True, True, True, True), adopted=True)
    # 直近5回中3回失敗 -> 過半数なので不採用に戻る
    for _ in range(3):
        state = update_adoption_state(state, False, window=5)
    assert state.adopted is False
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd python-engine && uv run pytest tests/test_walk_forward.py tests/test_adoption_state.py -v`
Expected: FAIL（モジュールが存在しない）

- [ ] **Step 3: `pyproject.toml`に`scipy`を追加する**

```toml
# python-engine/pyproject.toml の dependencies に scipy を追加
dependencies = [
    "fastapi>=0.115.0",
    "uvicorn[standard]>=0.30.0",
    "pydantic>=2.9.0",
    "pandas>=2.2.0",
    "pandas-ta>=0.3.14b",
    "httpx>=0.27.0",
    "scipy>=1.14.0",
]
```

Run: `cd python-engine && uv sync`

- [ ] **Step 4: 最小実装を書く**

```python
# python-engine/app/services/walk_forward.py
from dataclasses import dataclass

import numpy as np
from scipy import stats


@dataclass(frozen=True)
class WalkForwardFold:
    train_dates: list[str]
    test_dates: list[str]


def make_walk_forward_folds(
    sorted_unique_dates: list[str], n_folds: int, embargo: int
) -> list[WalkForwardFold]:
    """
    sorted_unique_dates: プール後の一意な日付の昇順リスト。
    拡大窓walk-forward: フォールドiの学習データは「フォールドiより前の全期間」、
    検証データは「フォールドi自身の期間」。学習末尾と検証開始の間に embargo 日分の
    空白を設け、ホライズン分のラベルが検証期間の価格を参照するリークを防ぐ。
    学習期間がembargoの3倍未満のフォールドは統計的に無意味なため除外する。
    """
    n = len(sorted_unique_dates)
    if n_folds < 2 or n == 0:
        return []
    fold_size = max(n // n_folds, 1)

    folds: list[WalkForwardFold] = []
    for i in range(1, n_folds):
        test_start = i * fold_size
        if test_start >= n:
            break
        test_end = n if i == n_folds - 1 else min((i + 1) * fold_size, n)
        train_end = test_start - embargo
        if train_end < embargo * 3:
            continue
        folds.append(
            WalkForwardFold(
                train_dates=sorted_unique_dates[:train_end],
                test_dates=sorted_unique_dates[test_start:test_end],
            )
        )
    return folds


def non_overlapping_sample(sorted_dates: list[str], step: int) -> list[str]:
    """
    ホライズン分（step）の間隔を空けた非重複な日付だけを抽出する。
    隣接ラベルが同じ将来価格を参照して自己相関することによる検定のp値過小評価を防ぐ。
    """
    return sorted_dates[::step]


def paired_significance_test(model_correct: np.ndarray, baseline_correct: np.ndarray) -> float:
    """
    対比較検定（厳密二項検定によるMcNemar検定）。
    b = ベースラインだけ的中した件数、c = モデルだけ的中した件数。
    帰無仮説「bとcは同確率」に対する片側p値（モデルが優れている方向）を返す。
    食い違いが一件もない場合は優位性なしとしてp値1.0を返す。
    """
    b = int(np.sum(baseline_correct & ~model_correct))
    c = int(np.sum(model_correct & ~baseline_correct))
    if b + c == 0:
        return 1.0
    return stats.binomtest(c, b + c, p=0.5, alternative="greater").pvalue
```

```python
# python-engine/app/services/adoption_state.py
from dataclasses import dataclass


@dataclass(frozen=True)
class AdoptionState:
    """直近window回の再学習サイクルでの採用基準充足履歴と、現在の採用可否。"""

    recent_results: tuple[bool, ...] = ()
    adopted: bool = False


def update_adoption_state(state: AdoptionState, meets_criteria: bool, window: int) -> AdoptionState:
    """
    24時間ごとの再検定をそのままadopted判定に使うと多重検定でα膨張するため、
    直近window回のサイクルの結果でヒステリシスをかける。
    - 未採用→採用: 直近window回**連続**で基準を満たした場合のみ
    - 採用→未採用: 直近window回のうち**過半数**が基準を満たさなくなった場合のみ
    - それ以外: 現状維持
    """
    history = (state.recent_results + (meets_criteria,))[-window:]
    if state.adopted:
        failed = sum(1 for r in history if not r)
        new_adopted = failed <= len(history) // 2
    else:
        new_adopted = len(history) == window and all(history)
    return AdoptionState(recent_results=history, adopted=new_adopted)
```

- [ ] **Step 5: テストが通ることを確認する**

Run: `cd python-engine && uv run pytest tests/test_walk_forward.py tests/test_adoption_state.py -v`
Expected: PASS（全11件）

- [ ] **Step 6: コミット**

```bash
git add python-engine/app/services/walk_forward.py python-engine/app/services/adoption_state.py \
        python-engine/tests/test_walk_forward.py python-engine/tests/test_adoption_state.py \
        python-engine/pyproject.toml python-engine/uv.lock
git commit -m "$(cat <<'EOF'
feat(python-engine): add walk-forward folds, paired significance test, and adoption hysteresis

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: モデル学習・評価・インメモリ状態管理（python-engine）

**Files:**
- Create: `python-engine/app/services/direction_model.py`
- Test: `python-engine/tests/test_direction_model.py`
- Modify: `python-engine/pyproject.toml`（`scikit-learn`を追加）

**Interfaces:**
- Consumes: `StockPriceSeries`, `build_pooled_dataset`（Task 2）、`FEATURE_COLUMNS`（Task 1）、`make_walk_forward_folds`, `non_overlapping_sample`, `paired_significance_test`（Task 3）、`AdoptionState`, `update_adoption_state`（Task 3）
- Produces:
  - `EvaluationResult`（dataclass: `hit_rate: float | None`, `baseline_hit_rate: float | None`, `p_value: float | None`, `independent_sample_count: int`, `meets_criteria: bool`, `pipeline: tuple[StandardScaler, LogisticRegression] | None`）
  - `evaluate_direction_model(stocks: list[StockPriceSeries], horizon: int = FORECAST_HORIZON) -> EvaluationResult`（学習・walk-forward検証を行う純粋関数。ヒステリシスは適用しない）
  - `DirectionModelStatus`（dataclass: `adopted: bool`, `hit_rate: float | None`, `baseline_hit_rate: float | None`, `p_value: float | None`, `independent_sample_count: int | None`, `trained_at: str | None`, `recent_results: tuple[bool, ...]`）
  - `DirectionModelState`（クラス。`status() -> DirectionModelStatus`, `try_begin_training() -> bool`, `train(stocks: list[StockPriceSeries]) -> DirectionModelStatus`, `predict_direction(features: dict[str, float]) -> str | None`, `reset() -> None`）
  - `direction_model_state: DirectionModelState`（モジュールレベルのシングルトン）

- [ ] **Step 1: 失敗するテストを書く**

```python
# python-engine/tests/test_direction_model.py
import datetime

import pytest

from app.services.direction_dataset import StockPriceSeries
from app.services.direction_model import (
    DirectionModelState,
    evaluate_direction_model,
)


def _dates(n: int, start: str = "2024-01-01") -> list[str]:
    d = datetime.date.fromisoformat(start)
    return [(d + datetime.timedelta(days=i)).isoformat() for i in range(n)]


def _trending_series(stock_code: str, n: int, daily_return: float) -> StockPriceSeries:
    prices = [1000.0 * (1 + daily_return) ** i for i in range(n)]
    return StockPriceSeries(stock_code=stock_code, dates=_dates(n), prices=prices)


def test_evaluate_direction_model_insufficient_rows_does_not_crash():
    # 1銘柄・短期間の履歴では学習データが足りず meets_criteria=False になる
    stocks = [_trending_series("7203", 50, 0.01)]
    result = evaluate_direction_model(stocks)
    assert result.meets_criteria is False
    assert result.pipeline is None


def test_evaluate_direction_model_with_enough_data_produces_stats():
    stocks = [
        _trending_series("7203", 400, 0.01),
        _trending_series("6758", 400, -0.005),
        _trending_series("9984", 400, 0.003),
    ]
    result = evaluate_direction_model(stocks)
    assert result.independent_sample_count > 0
    assert result.hit_rate is not None
    assert 0.0 <= result.hit_rate <= 1.0
    assert result.baseline_hit_rate is not None
    assert result.p_value is not None
    assert 0.0 <= result.p_value <= 1.0


def test_direction_model_state_starts_untrained():
    state = DirectionModelState()
    status = state.status()
    assert status.adopted is False
    assert state.predict_direction({"momentum_5": 0.0, "momentum_20": 0.0, "momentum_60": 0.0,
                                     "rsi_14": 50.0, "macd_hist": 0.0, "volatility_20": 0.01}) is None


def test_direction_model_state_try_begin_training_prevents_reentry():
    state = DirectionModelState()
    assert state.try_begin_training() is True
    assert state.try_begin_training() is False
    state.reset()
    assert state.try_begin_training() is True


def test_direction_model_state_train_updates_status():
    state = DirectionModelState()
    stocks = [
        _trending_series("7203", 400, 0.01),
        _trending_series("6758", 400, -0.005),
        _trending_series("9984", 400, 0.003),
    ]
    status = state.train(stocks)
    assert status.trained_at is not None
    assert status.independent_sample_count is not None and status.independent_sample_count > 0
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd python-engine && uv run pytest tests/test_direction_model.py -v`
Expected: FAIL（`app.services.direction_model` が存在しない）

- [ ] **Step 3: `pyproject.toml`に`scikit-learn`を追加する**

```toml
# python-engine/pyproject.toml の dependencies に scikit-learn を追加
dependencies = [
    "fastapi>=0.115.0",
    "uvicorn[standard]>=0.30.0",
    "pydantic>=2.9.0",
    "pandas>=2.2.0",
    "pandas-ta>=0.3.14b",
    "httpx>=0.27.0",
    "scipy>=1.14.0",
    "scikit-learn>=1.5.0",
]
```

Run: `cd python-engine && uv sync`

- [ ] **Step 4: 最小実装を書く**

```python
# python-engine/app/services/direction_model.py
import threading
from dataclasses import dataclass, field
from datetime import datetime, timezone

import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.preprocessing import StandardScaler

from app.schemas import FORECAST_HORIZON
from app.services.direction_dataset import StockPriceSeries, build_pooled_dataset
from app.services.direction_features import FEATURE_COLUMNS
from app.services.adoption_state import AdoptionState, update_adoption_state
from app.services.walk_forward import (
    make_walk_forward_folds,
    non_overlapping_sample,
    paired_significance_test,
)

MIN_TRAINING_ROWS = 200
MIN_INDEPENDENT_SAMPLES = 60
N_FOLDS = 5
HYSTERESIS_WINDOW = 5
SIGNIFICANCE_LEVEL = 0.05
MIN_FOLD_TRAIN_ROWS = 30

_Pipeline = tuple[StandardScaler, LogisticRegression]


@dataclass
class EvaluationResult:
    hit_rate: float | None
    baseline_hit_rate: float | None
    p_value: float | None
    independent_sample_count: int
    meets_criteria: bool
    pipeline: _Pipeline | None


def _fit_pipeline(frame, feature_columns: list[str]) -> _Pipeline:
    scaler = StandardScaler().fit(frame[feature_columns])
    clf = LogisticRegression(max_iter=1000).fit(scaler.transform(frame[feature_columns]), frame["label"])
    return scaler, clf


def evaluate_direction_model(
    stocks: list[StockPriceSeries], horizon: int = FORECAST_HORIZON
) -> EvaluationResult:
    """
    walk-forwardで既存ドリフト中心線に対する優位性を検証する純粋関数。
    ヒステリシス（連続再学習にまたがる採用判定）は呼び出し側（DirectionModelState）が行う。
    """
    dataset = build_pooled_dataset(stocks, horizon)
    if len(dataset) < MIN_TRAINING_ROWS:
        return EvaluationResult(None, None, None, 0, False, None)

    sorted_dates = sorted(dataset["date"].unique())
    folds = make_walk_forward_folds(sorted_dates, n_folds=N_FOLDS, embargo=horizon)

    model_correct_all: list[bool] = []
    baseline_correct_all: list[bool] = []

    for fold in folds:
        train_df = dataset[dataset["date"].isin(fold.train_dates)]
        test_df = dataset[dataset["date"].isin(fold.test_dates)]
        if len(train_df) < MIN_FOLD_TRAIN_ROWS or len(test_df) == 0:
            continue

        sampled_dates = set(non_overlapping_sample(sorted(test_df["date"].unique()), step=horizon))
        test_df = test_df[test_df["date"].isin(sampled_dates)]
        if len(test_df) == 0:
            continue

        scaler, clf = _fit_pipeline(train_df, FEATURE_COLUMNS)
        predictions = clf.predict(scaler.transform(test_df[FEATURE_COLUMNS]))

        model_correct_all.extend((predictions == test_df["label"]).tolist())
        baseline_correct_all.extend((test_df["baseline_direction"] == test_df["label"]).tolist())

    independent_sample_count = len(model_correct_all)
    if independent_sample_count < MIN_INDEPENDENT_SAMPLES:
        return EvaluationResult(None, None, None, independent_sample_count, False, None)

    model_correct = np.array(model_correct_all)
    baseline_correct = np.array(baseline_correct_all)
    hit_rate = float(model_correct.mean())
    baseline_hit_rate = float(baseline_correct.mean())
    p_value = paired_significance_test(model_correct, baseline_correct)

    meets_criteria = hit_rate > baseline_hit_rate and p_value < SIGNIFICANCE_LEVEL
    pipeline = _fit_pipeline(dataset, FEATURE_COLUMNS)

    return EvaluationResult(hit_rate, baseline_hit_rate, p_value, independent_sample_count, meets_criteria, pipeline)


@dataclass
class DirectionModelStatus:
    adopted: bool = False
    hit_rate: float | None = None
    baseline_hit_rate: float | None = None
    p_value: float | None = None
    independent_sample_count: int | None = None
    trained_at: str | None = None
    recent_results: tuple[bool, ...] = ()


class DirectionModelState:
    """python-engineプロセス内でモデルとその検証結果を保持するインメモリの状態。
    /model/train（書き込み）と /forecast（読み込み）が別スレッドから同時にアクセスしうるため
    threading.Lock で排他制御する。"""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._pipeline: _Pipeline | None = None
        self._status = DirectionModelStatus()
        self._adoption = AdoptionState()
        self._training = False

    def status(self) -> DirectionModelStatus:
        with self._lock:
            return self._status

    def try_begin_training(self) -> bool:
        """既に学習中ならFalseを返して即座に諦める（/model/train の多重起動対策）。"""
        with self._lock:
            if self._training:
                return False
            self._training = True
            return True

    def train(self, stocks: list[StockPriceSeries]) -> DirectionModelStatus:
        try:
            result = evaluate_direction_model(stocks)
            with self._lock:
                self._adoption = update_adoption_state(self._adoption, result.meets_criteria, HYSTERESIS_WINDOW)
                if result.pipeline is not None:
                    self._pipeline = result.pipeline
                self._status = DirectionModelStatus(
                    adopted=self._adoption.adopted,
                    hit_rate=result.hit_rate,
                    baseline_hit_rate=result.baseline_hit_rate,
                    p_value=result.p_value,
                    independent_sample_count=result.independent_sample_count,
                    trained_at=datetime.now(timezone.utc).isoformat(),
                    recent_results=self._adoption.recent_results,
                )
                return self._status
        finally:
            with self._lock:
                self._training = False

    def predict_direction(self, features: dict[str, float]) -> str | None:
        """学習済みモデルがあれば方向を返す（adoptedかどうかは呼び出し側が判断する）。
        未学習ならNone。"""
        with self._lock:
            pipeline = self._pipeline
        if pipeline is None:
            return None
        scaler, clf = pipeline
        row = [[features[col] for col in FEATURE_COLUMNS]]
        prediction = clf.predict(scaler.transform(row))[0]
        return "up" if prediction == 1 else "down"

    def reset(self) -> None:
        """テスト用: 状態を初期化する。"""
        with self._lock:
            self._pipeline = None
            self._status = DirectionModelStatus()
            self._adoption = AdoptionState()
            self._training = False


direction_model_state = DirectionModelState()
```

- [ ] **Step 5: テストが通ることを確認する**

Run: `cd python-engine && uv run pytest tests/test_direction_model.py -v`
Expected: PASS（全6件）

- [ ] **Step 6: コミット**

```bash
git add python-engine/app/services/direction_model.py python-engine/tests/test_direction_model.py \
        python-engine/pyproject.toml python-engine/uv.lock
git commit -m "$(cat <<'EOF'
feat(python-engine): add direction model training, evaluation, and in-memory state

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: ネガティブコントロールテスト（統計的妥当性の安全網）

**Files:**
- Create: `python-engine/tests/test_negative_control.py`

**Interfaces:**
- Consumes: `StockPriceSeries`（Task 2）、`evaluate_direction_model`（Task 4）

このタスクはコード追加なし・テストのみ。目的（設計書セクション4）は、方向を持たない合成データに
対して採用基準（`meets_criteria`）が有意水準αに近い頻度でしか真にならないことを確認し、
p値過小評価バグの安全網とすること。

- [ ] **Step 1: テストを書く**

```python
# python-engine/tests/test_negative_control.py
import datetime

import numpy as np
import pytest

from app.services.direction_dataset import StockPriceSeries
from app.services.direction_model import evaluate_direction_model


def _dates(n: int) -> list[str]:
    start = datetime.date(2020, 1, 1)
    return [(start + datetime.timedelta(days=i)).isoformat() for i in range(n)]


def _gbm_prices(rng: np.random.Generator, n_days: int, start_price: float = 1000.0) -> list[float]:
    """方向に関する情報を一切持たない、ドリフトゼロの幾何ブラウン運動系列を生成する。"""
    daily_returns = rng.normal(loc=0.0, scale=0.02, size=n_days)
    return (start_price * np.exp(np.cumsum(daily_returns))).tolist()


@pytest.mark.slow
def test_negative_control_false_positive_rate_matches_significance_level():
    """
    方向を持たない合成データに対して、採用基準（meets_criteria）が有意水準αに
    近い頻度でしか真にならないことを確認する。この検定が壊れていると
    （p値が過小評価されるバグがあると）ここで異常に高い採用率として現れる。
    """
    n_trials = 50
    n_stocks = 4
    n_days = 300

    false_positives = 0
    for seed in range(n_trials):
        rng = np.random.default_rng(seed)
        dates = _dates(n_days)
        stocks = [
            StockPriceSeries(stock_code=f"TEST{i}", dates=dates, prices=_gbm_prices(rng, n_days))
            for i in range(n_stocks)
        ]
        result = evaluate_direction_model(stocks)
        if result.meets_criteria:
            false_positives += 1

    false_positive_rate = false_positives / n_trials
    # 有意水準0.05に対し、二項分布のばらつきを考慮した緩い上限（0.20）で判定する。
    # p値過小評価バグがあれば採用率は50%超など明らかに逸脱した値になるため、
    # この緩い上限でも検出力は十分。
    assert false_positive_rate <= 0.20, (
        f"採用率が有意水準から乖離: {false_positive_rate:.2%}（p値が過小評価されている可能性）"
    )
```

- [ ] **Step 2: テストを実行して仕様を満たすことを確認する**

Run: `cd python-engine && uv run pytest tests/test_negative_control.py -v -m slow`
Expected: PASS。もしFAILする場合、Task 3/4の対比較検定・fold分割・非重複サンプリングの
いずれかにp値過小評価のバグがある可能性が高いため、実装に戻って調査する（このタスクを
「実装が正しいことを示すテスト」を先に書き、通らないので実装を直す、という順で進めても良い）。

`pytest.ini_options`に`slow`マーカーが未登録の場合は警告が出るため、`pyproject.toml`の
`[tool.pytest.ini_options]`に以下を追加する:

```toml
[tool.pytest.ini_options]
testpaths = ["tests"]
markers = ["slow: marks tests as slow (deselect with '-m \"not slow\"')"]
```

- [ ] **Step 3: コミット**

```bash
git add python-engine/tests/test_negative_control.py python-engine/pyproject.toml
git commit -m "$(cat <<'EOF'
test(python-engine): add negative-control test for direction model significance test

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `/forecast`への統合（中心線の方向差し替え）

**Files:**
- Modify: `python-engine/app/schemas.py`
- Modify: `python-engine/app/services/forecast.py`
- Modify: `python-engine/tests/test_forecast_service.py`
- Modify: `python-engine/tests/test_forecast_endpoint.py`

**Interfaces:**
- Consumes: `direction_model_state`（Task 4）、`compute_feature_matrix`, `FEATURE_COLUMNS`（Task 1）
- Produces: `ForecastResponse.direction_model: DirectionModelInfo`（常に存在、nullにはしない）

- [ ] **Step 1: 失敗するテストを書く**

```python
# python-engine/tests/test_forecast_service.py に追記
import pytest

from app.services.direction_model import direction_model_state
from app.services.forecast import calculate_forecast


@pytest.fixture(autouse=True)
def _reset_direction_model_state():
    direction_model_state.reset()
    yield
    direction_model_state.reset()


def test_calculate_forecast_direction_model_is_not_adopted_by_default():
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]
    result = calculate_forecast("7203", prices[-1], prices)
    assert result.direction_model.adopted is False
    assert result.direction_model.hit_rate is None


def test_calculate_forecast_center_unchanged_when_not_adopted():
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]
    result = calculate_forecast("7203", prices[-1], prices)
    # 既存の計算式通り: center = current_price * exp(mu*h)、上昇トレンドなのでcenterは上向き
    assert result.points[0].center > prices[-1]


def test_calculate_forecast_flips_center_sign_when_adopted_and_disagrees():
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]  # 上昇トレンド（mu > 0）

    class _FakePipeline:
        pass

    # direction_model_stateを「採用済み・down予測」に強制する
    direction_model_state._pipeline = (_FakePipeline(), _FakePipeline())
    direction_model_state.predict_direction = lambda features: "down"
    direction_model_state._status.adopted = True

    result = calculate_forecast("7203", prices[-1], prices)
    # 元のmu(>0)を符号反転しただけなので、centerは現在値より低くなる
    assert result.points[0].center < prices[-1]
    assert result.direction_model.adopted is True
    assert result.direction_model.predicted_direction == "down"
```

```python
# python-engine/tests/test_forecast_endpoint.py に追記
def test_forecast_response_always_includes_direction_model(client):
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]
    response = client.post(
        "/forecast",
        json={"stock_code": "7203", "current_price": prices[-1], "prices": prices},
    )
    body = response.json()
    assert "direction_model" in body
    assert body["direction_model"]["adopted"] is False
    assert body["direction_model"]["predicted_direction"] is None
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd python-engine && uv run pytest tests/test_forecast_service.py tests/test_forecast_endpoint.py -v`
Expected: FAIL（`direction_model`属性・フィールドが存在しない）

- [ ] **Step 3: `schemas.py`に`DirectionModelInfo`を追加し`ForecastResponse`を拡張する**

```python
# python-engine/app/schemas.py に追記（ForecastResponseの直前に配置し、ForecastResponseを修正）
class DirectionModelInfo(BaseModel):
    adopted: bool
    predicted_direction: str | None
    hit_rate: float | None
    baseline_hit_rate: float | None
    p_value: float | None
    independent_sample_count: int | None
    trained_at: str | None


class ForecastResponse(BaseModel):
    stock_code: str
    horizon: int
    points: list[ForecastPoint]
    direction_model: DirectionModelInfo
```

- [ ] **Step 4: `forecast.py`を修正する**

```python
# python-engine/app/services/forecast.py
import math

from app.schemas import (
    ESTIMATION_WINDOW,
    FORECAST_HORIZON,
    DirectionModelInfo,
    ForecastPoint,
    ForecastResponse,
)
from app.services.direction_features import FEATURE_COLUMNS, compute_feature_matrix
from app.services.direction_model import direction_model_state

# 68%区間・95%区間のz値。正規分布の標準的な信頼区間の目安として採用。
_Z_68 = 1.0
_Z_95 = 1.96


def _build_direction_model_info(prices: list[float]) -> tuple[DirectionModelInfo, str | None]:
    """
    現在の分類器の状態から、この銘柄向けのdirection_model情報と、
    中心線を差し替えるべき方向（'up'/'down'/None）を組み立てる。
    """
    status = direction_model_state.status()

    predicted_direction = None
    features_row = compute_feature_matrix(prices).iloc[-1]
    if not features_row.isna().any():
        predicted_direction = direction_model_state.predict_direction(features_row[FEATURE_COLUMNS].to_dict())

    info = DirectionModelInfo(
        adopted=status.adopted,
        predicted_direction=predicted_direction,
        hit_rate=status.hit_rate,
        baseline_hit_rate=status.baseline_hit_rate,
        p_value=status.p_value,
        independent_sample_count=status.independent_sample_count,
        trained_at=status.trained_at,
    )
    override_direction = predicted_direction if status.adopted else None
    return info, override_direction


def calculate_forecast(
    stock_code: str, current_price: float, prices: list[float]
) -> ForecastResponse:
    """
    直近 ESTIMATION_WINDOW 営業日の対数リターンから、ドリフト付きランダムウォークで
    FORECAST_HORIZON 営業日先までの中心線と68%/95%レンジ帯を計算する。
    方向分類器が採用済み(adopted)の場合、中心線の符号のみをその予測方向に合わせる
    （大きさ=|mu|は変更しない）。

    prices: 古い順（index 0 が最古）の終値リスト。最低 ESTIMATION_WINDOW + 1 件。
    """
    window = prices[-(ESTIMATION_WINDOW + 1) :]
    log_returns = [math.log(window[i] / window[i - 1]) for i in range(1, len(window))]

    mu = sum(log_returns) / len(log_returns)
    variance = sum((r - mu) ** 2 for r in log_returns) / len(log_returns)
    sigma = math.sqrt(variance)

    direction_info, override_direction = _build_direction_model_info(prices)
    mu_effective = mu
    if override_direction == "up" and mu <= 0:
        mu_effective = -mu
    elif override_direction == "down" and mu >= 0:
        mu_effective = -mu

    points: list[ForecastPoint] = []
    for h in range(1, FORECAST_HORIZON + 1):
        drift = mu_effective * h
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

    return ForecastResponse(
        stock_code=stock_code, horizon=FORECAST_HORIZON, points=points, direction_model=direction_info
    )
```

- [ ] **Step 5: テストが通ることを確認する**

Run: `cd python-engine && uv run pytest tests/test_forecast_service.py tests/test_forecast_endpoint.py -v`
Expected: PASS（既存テスト含め全件）

- [ ] **Step 6: コミット**

```bash
git add python-engine/app/schemas.py python-engine/app/services/forecast.py \
        python-engine/tests/test_forecast_service.py python-engine/tests/test_forecast_endpoint.py
git commit -m "$(cat <<'EOF'
feat(python-engine): integrate direction model into /forecast response

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `/model/train`・`/model/status`エンドポイント

**Files:**
- Modify: `python-engine/app/schemas.py`
- Create: `python-engine/app/routers/model.py`
- Modify: `python-engine/app/main.py`
- Test: `python-engine/tests/test_model_router.py`

**Interfaces:**
- Consumes: `direction_model_state`（Task 4）、`StockPriceSeries`（Task 2）
- Produces: `POST /model/train`（200/409）, `GET /model/status`（200）

- [ ] **Step 1: 失敗するテストを書く**

```python
# python-engine/tests/test_model_router.py
import pytest
from fastapi.testclient import TestClient

from app.main import app
from app.services.direction_model import direction_model_state


@pytest.fixture(scope="function")
def client():
    direction_model_state.reset()
    with TestClient(app) as c:
        yield c
    direction_model_state.reset()


def _train_payload(n_stocks: int = 3, n_days: int = 400) -> dict:
    import datetime

    start = datetime.date(2024, 1, 1)
    dates = [(start + datetime.timedelta(days=i)).isoformat() for i in range(n_days)]
    return {
        "stocks": [
            {
                "stock_code": f"TEST{i}",
                "prices": [
                    {"date": d, "close": 1000.0 * (1 + 0.005 * ((-1) ** i)) ** j}
                    for j, d in enumerate(dates)
                ],
            }
            for i in range(n_stocks)
        ]
    }


def test_model_train_success(client):
    response = client.post("/model/train", json=_train_payload())
    assert response.status_code == 200
    body = response.json()
    assert "adopted" in body
    assert "independent_sample_count" in body


def test_model_train_rejects_concurrent_calls(client):
    assert direction_model_state.try_begin_training() is True
    response = client.post("/model/train", json=_train_payload())
    assert response.status_code == 409
    direction_model_state.reset()


def test_model_status_before_training_is_untrained(client):
    response = client.get("/model/status")
    assert response.status_code == 200
    body = response.json()
    assert body["adopted"] is False
    assert body["trained_at"] is None
    assert body["recent_results"] == []


def test_model_status_after_training_reflects_result(client):
    client.post("/model/train", json=_train_payload())
    response = client.get("/model/status")
    body = response.json()
    assert body["trained_at"] is not None
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd python-engine && uv run pytest tests/test_model_router.py -v`
Expected: FAIL（`/model/train`・`/model/status`が存在しない）

- [ ] **Step 3: `schemas.py`にモデル学習用スキーマを追加する**

```python
# python-engine/app/schemas.py に追記
class ModelTrainPricePoint(BaseModel):
    date: str
    close: float


class ModelTrainStock(BaseModel):
    stock_code: str
    prices: list[ModelTrainPricePoint]


class ModelTrainRequest(BaseModel):
    stocks: list[ModelTrainStock]


class ModelTrainResponse(BaseModel):
    adopted: bool
    hit_rate: float | None
    baseline_hit_rate: float | None
    p_value: float | None
    independent_sample_count: int | None
    trained_at: str | None


class ModelStatusResponse(BaseModel):
    adopted: bool
    hit_rate: float | None
    baseline_hit_rate: float | None
    p_value: float | None
    independent_sample_count: int | None
    trained_at: str | None
    recent_results: list[bool]
```

- [ ] **Step 4: `routers/model.py`を作成する**

```python
# python-engine/app/routers/model.py
from fastapi import APIRouter, HTTPException

from app.schemas import ModelStatusResponse, ModelTrainRequest, ModelTrainResponse
from app.services.direction_dataset import StockPriceSeries
from app.services.direction_model import direction_model_state

router = APIRouter()


@router.post("/model/train", response_model=ModelTrainResponse)
def train_model(req: ModelTrainRequest) -> ModelTrainResponse:
    if not direction_model_state.try_begin_training():
        raise HTTPException(status_code=409, detail="training already in progress")

    stocks = [
        StockPriceSeries(
            stock_code=s.stock_code,
            dates=[p.date for p in s.prices],
            prices=[p.close for p in s.prices],
        )
        for s in req.stocks
    ]
    status = direction_model_state.train(stocks)
    return ModelTrainResponse(
        adopted=status.adopted,
        hit_rate=status.hit_rate,
        baseline_hit_rate=status.baseline_hit_rate,
        p_value=status.p_value,
        independent_sample_count=status.independent_sample_count,
        trained_at=status.trained_at,
    )


@router.get("/model/status", response_model=ModelStatusResponse)
def model_status() -> ModelStatusResponse:
    status = direction_model_state.status()
    return ModelStatusResponse(
        adopted=status.adopted,
        hit_rate=status.hit_rate,
        baseline_hit_rate=status.baseline_hit_rate,
        p_value=status.p_value,
        independent_sample_count=status.independent_sample_count,
        trained_at=status.trained_at,
        recent_results=list(status.recent_results),
    )
```

- [ ] **Step 5: `main.py`にルーターを登録する**

```python
# python-engine/app/main.py
from fastapi import FastAPI
from app.routers import analyze as analyze_router
from app.routers import forecast as forecast_router
from app.routers import model as model_router

app = FastAPI()
app.include_router(analyze_router.router)
app.include_router(forecast_router.router)
app.include_router(model_router.router)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}
```

- [ ] **Step 6: テストが通ることを確認する**

Run: `cd python-engine && uv run pytest tests/test_model_router.py -v`
Expected: PASS（全4件）

- [ ] **Step 7: python-engine全体のテストを実行する**

Run: `cd python-engine && uv run pytest -v -m "not slow"`
Expected: PASS（全件、`test_negative_control.py`は`slow`マーカーで除外）

- [ ] **Step 8: コミット**

```bash
git add python-engine/app/schemas.py python-engine/app/routers/model.py python-engine/app/main.py \
        python-engine/tests/test_model_router.py
git commit -m "$(cat <<'EOF'
feat(python-engine): add POST /model/train and GET /model/status endpoints

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: go-apiドメインインターフェース・gatewayクライアント・forecast構造体拡張

**Files:**
- Create: `go-api/internal/domain/directionmodel/trainer.go`
- Modify: `go-api/internal/domain/forecast/forecaster.go`
- Modify: `go-api/internal/interface/gateway/python_engine_client.go`
- Modify: `go-api/internal/interface/gateway/python_engine_client_test.go`

**Interfaces:**
- Produces:
  - `directionmodel.PriceSeries{StockCode stock.StockCode, Quotes []stock.Quote}`
  - `directionmodel.TrainingResult{Adopted bool, HitRate *float64, BaselineHitRate *float64, PValue *float64, IndependentSampleCount *int}`
  - `directionmodel.Trainer`インターフェース: `Train(ctx, []PriceSeries) (TrainingResult, error)`
  - `forecast.DirectionModel{Adopted bool, PredictedDirection *string, HitRate *float64, BaselineHitRate *float64, PValue *float64, IndependentSampleCount *int, TrainedAt *string}`
  - `forecast.Forecast.DirectionModel forecast.DirectionModel`（既存構造体にフィールド追加）
  - `(*PythonEngineClient).Train(ctx, []directionmodel.PriceSeries) (directionmodel.TrainingResult, error)`

- [ ] **Step 1: 失敗するテストを書く**

```go
// go-api/internal/interface/gateway/python_engine_client_test.go に追記
func TestPythonEngineClient_Train(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /model/train", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		stocks := body["stocks"].([]interface{})
		require.Len(t, stocks, 1)
		first := stocks[0].(map[string]interface{})
		assert.Equal(t, "7203", first["stock_code"])
		prices := first["prices"].([]interface{})
		require.Len(t, prices, 2)
		firstPrice := prices[0].(map[string]interface{})
		assert.Equal(t, "2026-07-06", firstPrice["date"])
		assert.Equal(t, 3200.0, firstPrice["close"])

		hitRate := 0.55
		pValue := 0.01
		samples := 120
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"adopted":                  true,
			"hit_rate":                 hitRate,
			"baseline_hit_rate":        0.50,
			"p_value":                  pValue,
			"independent_sample_count": samples,
			"trained_at":               "2026-09-29T00:00:00Z",
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	series := []directionmodel.PriceSeries{
		{
			StockCode: code,
			Quotes: []stock.Quote{
				{Price: 3200.0, Date: "2026-07-06"},
				{Price: 3250.0, Date: "2026-07-07"},
			},
		},
	}

	got, err := client.Train(context.Background(), series)
	require.NoError(t, err)
	assert.True(t, got.Adopted)
	require.NotNil(t, got.HitRate)
	assert.InDelta(t, 0.55, *got.HitRate, 0.001)
	require.NotNil(t, got.IndependentSampleCount)
	assert.Equal(t, 120, *got.IndependentSampleCount)
}

func TestPythonEngineClient_Train_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /model/train", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.Train(context.Background(), []directionmodel.PriceSeries{
		{StockCode: code, Quotes: []stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}},
	})
	require.Error(t, err)
}

func TestPythonEngineClient_Forecast_IncludesDirectionModel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /forecast", func(w http.ResponseWriter, r *http.Request) {
		predictedDirection := "up"
		hitRate := 0.55
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"stock_code": "7203",
			"horizon":    20,
			"points":     []interface{}{},
			"direction_model": map[string]interface{}{
				"adopted":                  true,
				"predicted_direction":      predictedDirection,
				"hit_rate":                 hitRate,
				"baseline_hit_rate":        0.50,
				"p_value":                  0.01,
				"independent_sample_count": 120,
				"trained_at":               "2026-09-29T00:00:00Z",
			},
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.Forecast(context.Background(), code, 3300.0, make([]float64, 130))
	require.NoError(t, err)
	assert.True(t, got.DirectionModel.Adopted)
	require.NotNil(t, got.DirectionModel.PredictedDirection)
	assert.Equal(t, "up", *got.DirectionModel.PredictedDirection)
}
```

このタスクでは既存の`internal/interface/gateway/python_engine_client_test.go`のimportに
`"github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel"`を追加する。

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestPythonEngineClient_Train -v`
Expected: FAIL（`directionmodel`パッケージ・`Train`メソッドが存在しない）

- [ ] **Step 3: `directionmodel`ドメインパッケージを作成する**

```go
// go-api/internal/domain/directionmodel/trainer.go
package directionmodel

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// PriceSeries は学習用に日付付きの終値系列を1銘柄分まとめたもの。
type PriceSeries struct {
	StockCode stock.StockCode
	Quotes    []stock.Quote
}

// TrainingResult はwalk-forward検証の結果。観測性のためgo-api側のログに残す。
type TrainingResult struct {
	Adopted                bool
	HitRate                *float64
	BaselineHitRate        *float64
	PValue                 *float64
	IndependentSampleCount *int
}

// Trainer はプールした全銘柄の価格履歴から方向分類器を学習・walk-forward検証する。
// 実装は python-engine の POST /model/train を呼ぶ
// （docs/superpowers/specs/2026-09-29-chart-direction-classifier-design.md 参照）。
type Trainer interface {
	Train(ctx context.Context, series []PriceSeries) (TrainingResult, error)
}
```

- [ ] **Step 4: `forecast.Forecast`に`DirectionModel`を追加する**

```go
// go-api/internal/domain/forecast/forecaster.go
package forecast

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type Point struct {
	Step    int
	Center  float64
	Upper68 float64
	Lower68 float64
	Upper95 float64
	Lower95 float64
}

// DirectionModel はPhase 2のAI方向分類器の状態。Adoptedがfalseの場合、
// 他のフィールドはnilになりうる（未学習、またはwalk-forwardで既存の
// ドリフト中心線に対する優位性を示せなかった状態）。
// docs/superpowers/specs/2026-09-29-chart-direction-classifier-design.md 参照。
type DirectionModel struct {
	Adopted                bool
	PredictedDirection     *string
	HitRate                *float64
	BaselineHitRate        *float64
	PValue                 *float64
	IndependentSampleCount *int
	TrainedAt              *string
}

type Forecast struct {
	Horizon        int
	Points         []Point
	DirectionModel DirectionModel
}

// Forecaster は将来の価格レンジ（ドリフト＋ボラティリティ帯）を計算する。
// 点予測ではなく方向とレンジ帯を返す方針は
// docs/superpowers/specs/2026-09-24-stock-chart-forecast-design.md 参照。
type Forecaster interface {
	Forecast(ctx context.Context, code stock.StockCode, currentPrice float64, prices []float64) (Forecast, error)
}
```

- [ ] **Step 5: `python_engine_client.go`に`Train`メソッドと`direction_model`のDTOマッピングを追加する**

```go
// go-api/internal/interface/gateway/python_engine_client.go に追記
// import に "github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel" を追加

var _ directionmodel.Trainer = (*PythonEngineClient)(nil)

type modelTrainPricePointDTO struct {
	Date  string  `json:"date"`
	Close float64 `json:"close"`
}

type modelTrainStockDTO struct {
	StockCode string                    `json:"stock_code"`
	Prices    []modelTrainPricePointDTO `json:"prices"`
}

type modelTrainRequest struct {
	Stocks []modelTrainStockDTO `json:"stocks"`
}

type modelTrainResponseDTO struct {
	Adopted                bool     `json:"adopted"`
	HitRate                *float64 `json:"hit_rate"`
	BaselineHitRate        *float64 `json:"baseline_hit_rate"`
	PValue                 *float64 `json:"p_value"`
	IndependentSampleCount *int     `json:"independent_sample_count"`
}

func (c *PythonEngineClient) Train(ctx context.Context, series []directionmodel.PriceSeries) (directionmodel.TrainingResult, error) {
	stocks := make([]modelTrainStockDTO, len(series))
	for i, s := range series {
		prices := make([]modelTrainPricePointDTO, len(s.Quotes))
		for j, q := range s.Quotes {
			prices[j] = modelTrainPricePointDTO{Date: q.Date, Close: float64(q.Price)}
		}
		stocks[i] = modelTrainStockDTO{StockCode: s.StockCode.String(), Prices: prices}
	}

	reqBody, err := json.Marshal(modelTrainRequest{Stocks: stocks})
	if err != nil {
		return directionmodel.TrainingResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/model/train", bytes.NewReader(reqBody))
	if err != nil {
		return directionmodel.TrainingResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return directionmodel.TrainingResult{}, fmt.Errorf("call python engine: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return directionmodel.TrainingResult{}, fmt.Errorf("python engine returned status %d", resp.StatusCode)
	}

	var result modelTrainResponseDTO
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return directionmodel.TrainingResult{}, fmt.Errorf("decode response: %w", err)
	}

	return directionmodel.TrainingResult{
		Adopted:                result.Adopted,
		HitRate:                result.HitRate,
		BaselineHitRate:        result.BaselineHitRate,
		PValue:                 result.PValue,
		IndependentSampleCount: result.IndependentSampleCount,
	}, nil
}
```

既存の`forecastResponseDTO`・`Forecast()`メソッドを次のように変更する:

```go
type directionModelDTO struct {
	Adopted                bool     `json:"adopted"`
	PredictedDirection     *string  `json:"predicted_direction"`
	HitRate                *float64 `json:"hit_rate"`
	BaselineHitRate        *float64 `json:"baseline_hit_rate"`
	PValue                 *float64 `json:"p_value"`
	IndependentSampleCount *int     `json:"independent_sample_count"`
	TrainedAt              *string  `json:"trained_at"`
}

type forecastResponseDTO struct {
	StockCode      string             `json:"stock_code"`
	Horizon        int                `json:"horizon"`
	Points         []forecastPointDTO `json:"points"`
	DirectionModel directionModelDTO  `json:"direction_model"`
}
```

`Forecast()`メソッド末尾の戻り値構築部分を次のように変更する:

```go
	return forecast.Forecast{
		Horizon: result.Horizon,
		Points:  points,
		DirectionModel: forecast.DirectionModel{
			Adopted:                result.DirectionModel.Adopted,
			PredictedDirection:     result.DirectionModel.PredictedDirection,
			HitRate:                result.DirectionModel.HitRate,
			BaselineHitRate:        result.DirectionModel.BaselineHitRate,
			PValue:                 result.DirectionModel.PValue,
			IndependentSampleCount: result.DirectionModel.IndependentSampleCount,
			TrainedAt:              result.DirectionModel.TrainedAt,
		},
	}, nil
```

- [ ] **Step 6: テストが通ることを確認する**

Run: `cd go-api && go test -race ./internal/domain/... ./internal/interface/gateway/... -v`
Expected: PASS（新規3件＋既存全件）

- [ ] **Step 7: コミット**

```bash
git add go-api/internal/domain/directionmodel/trainer.go \
        go-api/internal/domain/forecast/forecaster.go \
        go-api/internal/interface/gateway/python_engine_client.go \
        go-api/internal/interface/gateway/python_engine_client_test.go
git commit -m "$(cat <<'EOF'
feat(go-api): add directionmodel.Trainer domain interface and gateway support

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `TrainDirectionModelUsecase`（go-api）

**Files:**
- Create: `go-api/internal/usecase/train_direction_model.go`
- Create: `go-api/internal/usecase/train_direction_model_test.go`
- Create: `go-api/internal/usecase/mock_direction_model_trainer_test.go`

**Interfaces:**
- Consumes: `watchlist.Repository.FindAllStockCodes`, `stock.PriceRepository.FindRecent`（既存）、`directionmodel.Trainer`（Task 8）、既存の`mockWatchlistRepository`・`MockPriceRepository`（`internal/usecase`パッケージの既存テストファイルで定義済み）
- Produces:
  - `usecase.NewTrainDirectionModelUsecase(watchlists watchlist.Repository, prices stock.PriceRepository, trainer directionmodel.Trainer) *TrainDirectionModelUsecase`
  - `(*TrainDirectionModelUsecase).Run(ctx context.Context) error`
  - `(*TrainDirectionModelUsecase).RunWithRetry(ctx context.Context)`
  - `(*TrainDirectionModelUsecase).RunPeriodically(ctx context.Context, interval time.Duration)`

- [ ] **Step 1: 失敗するテストを書く**

```go
// go-api/internal/usecase/mock_direction_model_trainer_test.go
package usecase_test

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel"
	"github.com/stretchr/testify/mock"
)

type MockDirectionModelTrainer struct{ mock.Mock }

func (m *MockDirectionModelTrainer) Train(ctx context.Context, series []directionmodel.PriceSeries) (directionmodel.TrainingResult, error) {
	args := m.Called(ctx, series)
	return args.Get(0).(directionmodel.TrainingResult), args.Error(1)
}
```

```go
// go-api/internal/usecase/train_direction_model_test.go
package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTrainDirectionModelUsecase_Run_Success(t *testing.T) {
	code7203, _ := stock.NewStockCode("7203")
	code6758, _ := stock.NewStockCode("6758")

	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code7203, code6758}, nil)

	quotes7203 := []stock.Quote{{Price: 3200, Date: "2026-07-06"}}
	quotes6758 := []stock.Quote{{Price: 1500, Date: "2026-07-06"}}
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code7203, mock.Anything).Return(quotes7203, nil)
	prices.On("FindRecent", mock.Anything, code6758, mock.Anything).Return(quotes6758, nil)

	trainer := new(MockDirectionModelTrainer)
	trainer.On("Train", mock.Anything, mock.MatchedBy(func(series []directionmodel.PriceSeries) bool {
		return len(series) == 2
	})).Return(directionmodel.TrainingResult{Adopted: false}, nil)

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)
	err := uc.Run(context.Background())

	require.NoError(t, err)
	trainer.AssertExpectations(t)
}

func TestTrainDirectionModelUsecase_Run_SkipsWhenNoStocksHaveHistory(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code}, nil)

	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return([]stock.Quote{}, nil)

	trainer := new(MockDirectionModelTrainer)

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)
	err := uc.Run(context.Background())

	require.NoError(t, err)
	trainer.AssertNotCalled(t, "Train", mock.Anything, mock.Anything)
}

func TestTrainDirectionModelUsecase_Run_PropagatesTrainerError(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code}, nil)

	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).
		Return([]stock.Quote{{Price: 3200, Date: "2026-07-06"}}, nil)

	trainer := new(MockDirectionModelTrainer)
	trainer.On("Train", mock.Anything, mock.Anything).
		Return(directionmodel.TrainingResult{}, errors.New("engine unavailable"))

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)
	err := uc.Run(context.Background())

	require.Error(t, err)
}

func TestTrainDirectionModelUsecase_RunWithRetry_SucceedsOnFirstAttempt(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code}, nil)

	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).
		Return([]stock.Quote{{Price: 3200, Date: "2026-07-06"}}, nil)

	trainer := new(MockDirectionModelTrainer)
	trainer.On("Train", mock.Anything, mock.Anything).
		Return(directionmodel.TrainingResult{Adopted: false}, nil).Once()

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)

	done := make(chan struct{})
	go func() {
		uc.RunWithRetry(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithRetry should return immediately on first success without waiting")
	}
	trainer.AssertNumberOfCalls(t, "Train", 1)
}

func TestTrainDirectionModelUsecase_RunPeriodically_StopsOnContextCancel(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code}, nil)

	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).
		Return([]stock.Quote{{Price: 3200, Date: "2026-07-06"}}, nil)

	trainer := new(MockDirectionModelTrainer)
	trainer.On("Train", mock.Anything, mock.Anything).
		Return(directionmodel.TrainingResult{Adopted: false}, nil)

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		uc.RunPeriodically(ctx, time.Hour)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPeriodically should return promptly after context cancellation")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd go-api && go test ./internal/usecase/... -run TestTrainDirectionModelUsecase -v`
Expected: FAIL（`TrainDirectionModelUsecase`が存在しない）

- [ ] **Step 3: 最小実装を書く**

```go
// go-api/internal/usecase/train_direction_model.go
package usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

// directionModelTrainTimeout はpython-engineの学習呼び出しに課す上限。
// ロジスティック回帰・全銘柄プールでも数百〜数千サンプル程度で、
// 既存の/forecast呼び出し（10秒）より重いが、数十秒あれば十分なはず。
const directionModelTrainTimeout = 60 * time.Second

// directionModelTrainRetryDelays は起動直後にpython-engineがまだ立ち上がっていない
// 場合のリトライ間隔。学習はモデル状態を持たない（失敗しても既存の統計手法に
// フォールバックするだけ）ため、BackfillPriceHistoryUsecaseほど強い自己回復は
// 不要だが、デプロイ直後の初回学習だけは取りこぼしたくないため数回リトライする。
var directionModelTrainRetryDelays = []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute}

type TrainDirectionModelUsecase struct {
	watchlists watchlist.Repository
	prices     stock.PriceRepository
	trainer    directionmodel.Trainer
}

func NewTrainDirectionModelUsecase(
	watchlists watchlist.Repository,
	prices stock.PriceRepository,
	trainer directionmodel.Trainer,
) *TrainDirectionModelUsecase {
	return &TrainDirectionModelUsecase{watchlists: watchlists, prices: prices, trainer: trainer}
}

// Run はwatchlist全銘柄の価格履歴を集めてpython-engineへ学習を依頼する。
// 履歴が1件も無い銘柄は除外する。全銘柄が対象外なら何もしない。
func (u *TrainDirectionModelUsecase) Run(ctx context.Context) error {
	codes, err := u.watchlists.FindAllStockCodes(ctx)
	if err != nil {
		return fmt.Errorf("fetch watchlist codes: %w", err)
	}

	var series []directionmodel.PriceSeries
	for _, code := range codes {
		quotes, err := u.prices.FindRecent(ctx, code, backfillDays)
		if err != nil {
			return fmt.Errorf("fetch price history %s: %w", code, err)
		}
		if len(quotes) == 0 {
			continue
		}
		series = append(series, directionmodel.PriceSeries{StockCode: code, Quotes: quotes})
	}
	if len(series) == 0 {
		return nil
	}

	trainCtx, cancel := context.WithTimeout(ctx, directionModelTrainTimeout)
	defer cancel()
	result, err := u.trainer.Train(trainCtx, series)
	if err != nil {
		return fmt.Errorf("train direction model: %w", err)
	}

	log.Printf(
		"direction model trained: adopted=%v hit_rate=%v baseline_hit_rate=%v p_value=%v samples=%v",
		result.Adopted, derefFloat(result.HitRate), derefFloat(result.BaselineHitRate),
		derefFloat(result.PValue), derefInt(result.IndependentSampleCount),
	)
	return nil
}

// RunWithRetry はRunが失敗した場合、directionModelTrainRetryDelaysの間隔でリトライする。
// python-engineがgo-apiより後に起動する場合のデプロイ順序問題に対応する。
// 全リトライ失敗時はログのみで次の定期サイクルへ委ねる（学習の失敗は既存機能を止めない）。
func (u *TrainDirectionModelUsecase) RunWithRetry(ctx context.Context) {
	if err := u.Run(ctx); err == nil {
		return
	} else {
		log.Printf("ERROR train direction model (attempt 1): %v", err)
	}

	for i, delay := range directionModelTrainRetryDelays {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if err := u.Run(ctx); err != nil {
			log.Printf("ERROR train direction model (attempt %d): %v", i+2, err)
			continue
		}
		return
	}
	log.Println("direction model training failed after all retries, will retry at next scheduled cycle")
}

// RunPeriodically はRunWithRetryを起動時に1回、以降interval間隔で実行し続ける。
// ctxがキャンセルされるまでブロックする。
func (u *TrainDirectionModelUsecase) RunPeriodically(ctx context.Context, interval time.Duration) {
	u.RunWithRetry(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.RunWithRetry(ctx)
		}
	}
}

func derefFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `cd go-api && go test -race ./internal/usecase/... -run TestTrainDirectionModelUsecase -v`
Expected: PASS（全5件）

- [ ] **Step 5: コミット**

```bash
git add go-api/internal/usecase/train_direction_model.go \
        go-api/internal/usecase/train_direction_model_test.go \
        go-api/internal/usecase/mock_direction_model_trainer_test.go
git commit -m "$(cat <<'EOF'
feat(go-api): add TrainDirectionModelUsecase with startup retry and periodic scheduling

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: main.go配線・チャートAPIレスポンスへの反映

**Files:**
- Modify: `go-api/cmd/api/main.go`
- Modify: `go-api/internal/interface/handler/stock_handler.go`
- Modify: `go-api/internal/interface/handler/stock_handler_test.go`
- Modify: `CLAUDE.md`（環境変数節）

**Interfaces:**
- Consumes: `usecase.NewTrainDirectionModelUsecase`（Task 9）、`forecast.Forecast.DirectionModel`（Task 8）

- [ ] **Step 1: 失敗するテストを書く**

```go
// go-api/internal/interface/handler/stock_handler_test.go に追記
func TestStockHandler_Chart_IncludesDirectionModel(t *testing.T) {
	predictedDirection := "up"
	hitRate := 0.55
	baselineHitRate := 0.50
	pValue := 0.01
	samples := 120
	trainedAt := "2026-09-29T00:00:00Z"

	h := handler.NewStockHandler(stubStockChartUsecase{result: usecase.StockChart{
		StockCode: "7203",
		Forecast: &forecast.Forecast{
			Horizon: 20,
			Points:  []forecast.Point{{Step: 1, Center: 1200}},
			DirectionModel: forecast.DirectionModel{
				Adopted:                true,
				PredictedDirection:     &predictedDirection,
				HitRate:                &hitRate,
				BaselineHitRate:        &baselineHitRate,
				PValue:                 &pValue,
				IndependentSampleCount: &samples,
				TrainedAt:              &trainedAt,
			},
		},
	}})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/stocks/7203/chart", nil), "user-1")
	req.SetPathValue("code", "7203")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	fc := resp["forecast"].(map[string]interface{})
	dm := fc["direction_model"].(map[string]interface{})
	assert.Equal(t, true, dm["adopted"])
	assert.Equal(t, "up", dm["predicted_direction"])
}
```

このタスクでは既存の`stock_handler_test.go`にある`stubStockChartUsecase`（構造体リテラルで
`result`/`err`を渡すスタブ）と`withUserContext`ヘルパーをそのまま再利用する。既存の
`TestStockHandler_Chart_Success`は`assert.Nil(t, resp["forecast"])`でforecastがnilの場合しか
確認していないため、本タスクの新規テストでforecastがnon-nilの場合の`direction_model`
シリアライズを初めて検証する。import に`"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"`を追加する。

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd go-api && go test ./internal/interface/handler/... -run TestStockHandler_Chart_IncludesDirectionModel -v`
Expected: FAIL（`direction_model`がレスポンスに含まれない）

- [ ] **Step 3: `stock_handler.go`を修正する**

```go
// go-api/internal/interface/handler/stock_handler.go に追記・修正
type directionModelResponse struct {
	Adopted                bool     `json:"adopted"`
	PredictedDirection     *string  `json:"predicted_direction"`
	HitRate                *float64 `json:"hit_rate"`
	BaselineHitRate        *float64 `json:"baseline_hit_rate"`
	PValue                 *float64 `json:"p_value"`
	IndependentSampleCount *int     `json:"independent_sample_count"`
	TrainedAt              *string  `json:"trained_at"`
}

type forecastResponse struct {
	Horizon        int                     `json:"horizon"`
	Points         []forecastPointResponse `json:"points"`
	DirectionModel directionModelResponse  `json:"direction_model"`
}
```

`toStockChartResponse`内、`fc = &forecastResponse{...}`の組み立てを次のように変更する:

```go
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
		fc = &forecastResponse{
			Horizon: c.Forecast.Horizon,
			Points:  points,
			DirectionModel: directionModelResponse{
				Adopted:                c.Forecast.DirectionModel.Adopted,
				PredictedDirection:     c.Forecast.DirectionModel.PredictedDirection,
				HitRate:                c.Forecast.DirectionModel.HitRate,
				BaselineHitRate:        c.Forecast.DirectionModel.BaselineHitRate,
				PValue:                 c.Forecast.DirectionModel.PValue,
				IndependentSampleCount: c.Forecast.DirectionModel.IndependentSampleCount,
				TrainedAt:              c.Forecast.DirectionModel.TrainedAt,
			},
		}
	}
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `cd go-api && go test -race ./internal/interface/handler/... -v`
Expected: PASS（新規テスト＋既存全件）

- [ ] **Step 5: `main.go`に環境変数・usecase配線・起動処理を追加する**

```go
// go-api/cmd/api/main.go の const 節に追記
const directionModelRetrainDefaultInterval = 24 * time.Hour
```

```go
// go-api/cmd/api/main.go のenv var読み込み部分（POLL_TIMEの読み込みの後）に追記
directionModelRetrainInterval := directionModelRetrainDefaultInterval
if v := os.Getenv("DIRECTION_MODEL_RETRAIN_INTERVAL"); v != "" {
    var err error
    directionModelRetrainInterval, err = time.ParseDuration(v)
    if err != nil {
        log.Fatalf("invalid DIRECTION_MODEL_RETRAIN_INTERVAL: %v", err)
    }
}
```

```go
// go-api/cmd/api/main.go のusecase生成部分（chartUsecaseの後）に追記
directionModelUsecase := usecase.NewTrainDirectionModelUsecase(watchlistRepo, priceRepo, pythonEngineClient)
```

```go
// go-api/cmd/api/main.go のHTTPサーバーgoroutine起動後、既存の起動時バックフィルgoroutineの後に追記
go func() {
    directionModelUsecase.RunPeriodically(ctx, directionModelRetrainInterval)
}()
```

- [ ] **Step 6: `CLAUDE.md`の環境変数節に`DIRECTION_MODEL_RETRAIN_INTERVAL`を追記する**

```markdown
## 環境変数（本番）
DATABASE_URL, ANOMALY_THRESHOLD（デフォルト2.5）, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）, DIRECTION_MODEL_RETRAIN_INTERVAL（デフォルト24h、AI方向分類器の再学習間隔。Go duration形式）
```

- [ ] **Step 7: go-api全体のビルド・単体テストを実行する**

Run: `cd go-api && go build ./... && go test -race -short ./...`
Expected: PASS（ビルド成功、全単体テスト成功）

- [ ] **Step 8: コミット**

```bash
git add go-api/cmd/api/main.go go-api/internal/interface/handler/stock_handler.go \
        go-api/internal/interface/handler/stock_handler_test.go CLAUDE.md
git commit -m "$(cat <<'EOF'
feat(go-api): wire up direction model training at startup and expose it via chart API

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: フロントエンドの型・バッジ表示

**Files:**
- Modify: `frontend/lib/go-api-client.ts`
- Modify: `frontend/components/stock-chart.tsx`
- Modify: `frontend/components/stock-chart.test.tsx`

**Interfaces:**
- Consumes: `StockChart.forecast.direction_model`（go-api、Task 10）
- Produces: `StockChartDirectionModel`型、`StockChart`コンポーネントのバッジ表示

- [ ] **Step 1: 失敗するテストを書く**

```tsx
// frontend/components/stock-chart.test.tsx に追記
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { StockChart } from "./stock-chart";
import type { StockChart as StockChartData } from "@/lib/go-api-client";

function baseChartData(overrides: Partial<StockChartData> = {}): StockChartData {
  return {
    stock_code: "7203",
    current_price: 3300,
    prices: [{ date: "2026-09-01", close: 3300 }],
    alert_band: [],
    current_z_score: null,
    forecast: {
      horizon: 20,
      points: [
        { step: 1, center: 3350, upper_68: 3400, lower_68: 3300, upper_95: 3450, lower_95: 3250 },
      ],
      direction_model: {
        adopted: false,
        predicted_direction: null,
        hit_rate: null,
        baseline_hit_rate: null,
        p_value: null,
        independent_sample_count: null,
        trained_at: null,
      },
    },
    notifications: [],
    ...overrides,
  };
}

describe("StockChart direction model badge", () => {
  it("adopted=falseの場合はバッジを表示しない", () => {
    render(<StockChart data={baseChartData()} />);
    expect(screen.queryByText(/AIモデルによる方向予測/)).not.toBeInTheDocument();
  });

  it("adopted=trueの場合はバッジと的中率を表示する", () => {
    const data = baseChartData();
    data.forecast!.direction_model = {
      adopted: true,
      predicted_direction: "up",
      hit_rate: 0.57,
      baseline_hit_rate: 0.5,
      p_value: 0.01,
      independent_sample_count: 120,
      trained_at: "2026-09-29T00:00:00Z",
    };
    render(<StockChart data={data} />);
    expect(screen.getByText(/AIモデルによる方向予測/)).toBeInTheDocument();
    expect(screen.getByText(/57%/)).toBeInTheDocument();
    expect(screen.getByText(/将来の的中を保証しない/)).toBeInTheDocument();
  });

  it("forecastがnullでもクラッシュしない", () => {
    render(<StockChart data={baseChartData({ forecast: null })} />);
    expect(screen.getByText("予測を取得できませんでした")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `cd frontend && npx vitest run components/stock-chart.test.tsx`
Expected: FAIL（`direction_model`型・バッジが存在しない）

- [ ] **Step 3: `lib/go-api-client.ts`に型を追加する**

```typescript
// frontend/lib/go-api-client.ts の StockChartForecast 定義を修正
export type StockChartDirectionModel = {
  adopted: boolean;
  predicted_direction: "up" | "down" | null;
  hit_rate: number | null;
  baseline_hit_rate: number | null;
  p_value: number | null;
  independent_sample_count: number | null;
  trained_at: string | null;
};
export type StockChartForecast = {
  horizon: number;
  points: StockChartForecastPoint[];
  direction_model: StockChartDirectionModel;
};
```

- [ ] **Step 4: `stock-chart.tsx`にバッジ表示を追加する**

```tsx
// frontend/components/stock-chart.tsx の StockChart コンポーネント内、
// <ResponsiveContainer> の直後・末尾の <p> タグの前に追記
export function StockChart({ data }: { data: StockChartData }) {
  const [period, setPeriod] = useState<ChartPeriod>(DEFAULT_PERIOD);
  const displayData = sliceToDisplayWindow(data, PERIOD_TRADING_DAYS[period]);
  const rows = buildRows(displayData);
  const markerRows = rows.filter(
    (r) => r.notification !== undefined && r.close !== undefined,
  );
  const directionModel = data.forecast?.direction_model;

  return (
    <div className="space-y-2">
      {/* ...既存の期間切替ボタン・予測取得失敗メッセージ・ResponsiveContainerはそのまま... */}
      {directionModel?.adopted && (
        <p className="text-xs text-muted-foreground" role="status">
          AIモデルによる方向予測（過去データでの的中率
          {Math.round((directionModel.hit_rate ?? 0) * 100)}%、既存手法比で統計的に有意）。
          この的中率は過去データでの検証結果であり、将来の的中を保証しないことにご注意ください。
        </p>
      )}
      <p className="text-xs text-muted-foreground">
        統計的期待レンジは直近120営業日の対数リターンの平均と標準偏差から算出した
        ドリフト＋ボラティリティ区間であり、価格予測ではありません。アラート境界は
        直近30日の平均±アラート閾値σ（実際に通知が発火する境界）を示します。
      </p>
    </div>
  );
}
```

（既存のJSXの他の部分は変更しない。上記は差分の要点のみを示す。実装時は既存ファイルの
該当箇所にこのdirectionModel変数の宣言とバッジの`<p>`要素を過不足なく挿入すること。）

- [ ] **Step 5: テストが通ることを確認する**

Run: `cd frontend && npx vitest run components/stock-chart.test.tsx`
Expected: PASS（新規3件＋既存全件）

- [ ] **Step 6: フロントエンド全体のテストと型チェックを実行する**

Run: `cd frontend && npx vitest run && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 7: コミット**

```bash
git add frontend/lib/go-api-client.ts frontend/components/stock-chart.tsx frontend/components/stock-chart.test.tsx
git commit -m "$(cat <<'EOF'
feat(frontend): show AI direction model badge when adopted

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## 実装後の確認

- [ ] `cd python-engine && uv run pytest -v`（`slow`マーカー含め全件）
- [ ] `cd go-api && go build ./... && go test -race -short ./...`
- [ ] `cd frontend && npx vitest run && npx tsc --noEmit`
- [ ] `CLAUDE.md`の環境変数節が更新されていることを確認
- [ ] `superpowers:requesting-code-review`でwhole-branchレビューを実施
