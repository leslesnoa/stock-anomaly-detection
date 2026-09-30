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
