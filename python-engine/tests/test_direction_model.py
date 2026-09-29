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
    # 900営業日×3銘柄: walk-forwardは4フォールド生成され、各フォールドの非重複サンプリングで
    # 約8件×3銘柄=24件、4フォールド合計で約96件の独立検定サンプルが確保される
    # （MIN_INDEPENDENT_SAMPLES=60を上回る）。400営業日では27件程度にしかならず
    # このガードに引っかかってhit_rate等がNoneのままになるため、意図的に大きくしている。
    stocks = [
        _trending_series("7203", 900, 0.01),
        _trending_series("6758", 900, -0.005),
        _trending_series("9984", 900, 0.003),
    ]
    result = evaluate_direction_model(stocks)
    assert result.independent_sample_count >= 60
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
