import datetime
from unittest.mock import patch

import pytest

from app.services.adoption_state import AdoptionState
from app.services.direction_dataset import StockPriceSeries
from app.services.direction_model import (
    MIN_HYSTERESIS_RECORD_INTERVAL,
    MIN_INDEPENDENT_SAMPLES,
    DirectionModelState,
    DirectionModelStatus,
    evaluate_direction_model,
)

_NEUTRAL_FEATURES = {
    "momentum_5": 0.0,
    "momentum_20": 0.0,
    "momentum_60": 0.0,
    "rsi_14": 50.0,
    "macd_hist": 0.0,
    "volatility_20": 0.01,
}


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
    # 1200営業日×3銘柄: walk-forwardは4フォールド生成され、各フォールドの非重複サンプリング後に
    # 日付単位へ集約されるため、独立サンプル数は実測44件（MIN_INDEPENDENT_SAMPLES=30を上回る）。
    # 集約は日付単位なので銘柄数を増やしてもこの件数は増えない。900営業日だと32件、
    # 400営業日だと9件しかなくガードに引っかかってhit_rate等がNoneのままになるため、
    # 意図的に余裕を持たせている。
    stocks = [
        _trending_series("7203", 1200, 0.01),
        _trending_series("6758", 1200, -0.005),
        _trending_series("9984", 1200, 0.003),
    ]
    result = evaluate_direction_model(stocks)
    assert result.independent_sample_count >= MIN_INDEPENDENT_SAMPLES
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


def _sufficient_stocks(n: int = 1200) -> list[StockPriceSeries]:
    return [
        _trending_series("7203", n, 0.01),
        _trending_series("6758", n, -0.005),
        _trending_series("9984", n, 0.003),
    ]


def test_direction_model_state_train_with_insufficient_data_resets_adoption_and_pipeline():
    """学習データ不足の回はヒステリシス窓に投票せず、採用状態とモデルを即座に捨てる。
    混ぜてしまうと adopted=True のまま hit_rate=None という内部矛盾した状態に到達しうる。"""
    state = DirectionModelState()
    # まず正常に学習してpipelineを持たせ、さらに「過去に採用済み」の前提を作る。
    state.train(_sufficient_stocks())
    with state._lock:
        state._adoption = AdoptionState(recent_results=(True,) * 5, adopted=True)
        state._status = DirectionModelStatus(
            adopted=True, hit_rate=0.6, baseline_hit_rate=0.5, p_value=0.01
        )
    assert state.predict_direction(_NEUTRAL_FEATURES) is not None

    # 学習データが足りない回（1銘柄・50日）
    status = state.train([_trending_series("7203", 50, 0.01)])

    assert status.adopted is False
    assert status.hit_rate is None
    assert status.baseline_hit_rate is None
    assert status.p_value is None
    assert status.recent_results == ()
    # ステータスのフィールドだけでなくモデル本体も破棄されていること
    assert state.predict_direction(_NEUTRAL_FEATURES) is None


def test_direction_model_state_consecutive_trains_record_single_hysteresis_vote():
    """起動直後の再起動連打で同じデータに対して複数票が入らないこと。"""
    state = DirectionModelState()
    stocks = _sufficient_stocks()

    first = state.train(stocks)
    assert len(first.recent_results) == 1

    second = state.train(stocks)
    assert len(second.recent_results) == 1, (
        "最小間隔内の再学習はヒステリシス窓に2票目を入れてはならない"
    )
    # モデル・統計値自体は更新される（投票だけを抑制している）
    assert second.trained_at is not None
    assert second.independent_sample_count == first.independent_sample_count


def test_direction_model_state_records_hysteresis_vote_after_min_interval():
    """最小間隔を超えて時間が経った再学習はきちんと次の票として記録される。"""
    state = DirectionModelState()
    stocks = _sufficient_stocks()

    first = state.train(stocks)
    assert len(first.recent_results) == 1

    later = datetime.datetime.now(datetime.timezone.utc) + MIN_HYSTERESIS_RECORD_INTERVAL
    with patch("app.services.direction_model.datetime") as mock_datetime:
        mock_datetime.now.return_value = later
        second = state.train(stocks)
    assert len(second.recent_results) == 2
