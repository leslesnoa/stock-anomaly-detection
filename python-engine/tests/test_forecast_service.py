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


def test_calculate_forecast_flips_center_sign_when_adopted_and_disagrees(monkeypatch):
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]  # 上昇トレンド（mu > 0）

    # direction_model_stateを「採用済み・down予測」に強制する。predict_directionの上書きは
    # monkeypatchで行う（reset()はDirectionModelStateが持つ属性しか戻さないため、
    # インスタンスに直接代入すると次のテストに漏れて汚染する）。
    monkeypatch.setattr(direction_model_state, "predict_direction", lambda features: "down")
    direction_model_state._status.adopted = True

    result = calculate_forecast("7203", prices[-1], prices)
    # 元のmu(>0)を符号反転しただけなので、centerは現在値より低くなる
    assert result.points[0].center < prices[-1]
    assert result.direction_model.adopted is True
    assert result.direction_model.predicted_direction == "down"
