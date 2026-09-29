import pytest
from pydantic import ValidationError
from app.schemas import AnalyzeRequest, AnalyzeResponse, Indicators, MACDValue, BollingerValue
from app.schemas import (
    DirectionModelInfo,
    ForecastRequest,
    ForecastResponse,
    ForecastPoint,
    ESTIMATION_WINDOW,
)


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
        direction_model=DirectionModelInfo(
            adopted=False,
            predicted_direction=None,
            hit_rate=None,
            baseline_hit_rate=None,
            p_value=None,
            independent_sample_count=None,
            trained_at=None,
        ),
    )
    assert resp.horizon == 20
    assert resp.points[0].step == 1
