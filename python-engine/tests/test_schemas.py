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
