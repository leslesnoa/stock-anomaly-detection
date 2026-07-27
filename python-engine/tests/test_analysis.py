from app.schemas import AnalyzeRequest, NewsItem
from app.services.analysis import AnalysisService
from app.services.news_client import MockNewsClient


def _make_request(n_prices: int = 30) -> AnalyzeRequest:
    return AnalyzeRequest(
        stock_code="7203",
        z_score=3.2,
        current_price=3250.0,
        prices=[3000.0 + i * 10 for i in range(n_prices)],
    )


def test_analyze_returns_response_with_indicators():
    svc = AnalysisService(news_client=MockNewsClient())
    resp = svc.analyze(_make_request(30))
    assert resp.stock_code == "7203"
    assert resp.indicators is not None
    assert resp.news == []


def test_analyze_includes_news_from_client():
    class StubNewsClient:
        def fetch_news(self, stock_code: str):
            return [NewsItem(title="テスト", url="https://example.com", published_at="2026-07-17T09:00:00Z")]

    svc = AnalysisService(news_client=StubNewsClient())
    resp = svc.analyze(_make_request(30))
    assert len(resp.news) == 1
    assert resp.news[0].title == "テスト"


def test_analyze_rsi_is_not_none_with_30_prices():
    svc = AnalysisService(news_client=MockNewsClient())
    resp = svc.analyze(_make_request(30))
    # 30件の単調増加 → RSI は計算できる
    assert resp.indicators.rsi is not None


def test_analyze_news_empty_on_client_failure():
    class FailingNewsClient:
        def fetch_news(self, stock_code: str):
            return []  # FinnhubNewsClient は例外をキャッチして [] を返す

    svc = AnalysisService(news_client=FailingNewsClient())
    resp = svc.analyze(_make_request(30))
    assert resp.news == []
    # 指標は正常に返る
    assert resp.indicators is not None
