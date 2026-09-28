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
