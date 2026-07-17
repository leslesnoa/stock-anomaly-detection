import pytest
from app.schemas import NewsItem
from app.services.news_client import FinnhubNewsClient, MockNewsClient


def test_mock_news_client_returns_empty_list():
    client = MockNewsClient()
    result = client.fetch_news("7203")
    assert result == []


def test_finnhub_client_returns_news_items(httpx_mock):
    httpx_mock.add_response(
        json=[
            {
                "headline": "トヨタが新型EV発表",
                "url": "https://example.com/news/1",
                "datetime": 1720000000,
            },
            {
                "headline": "決算発表",
                "url": "https://example.com/news/2",
                "datetime": 1720100000,
            },
        ]
    )
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert len(items) == 2
    assert items[0].title == "トヨタが新型EV発表"
    assert items[0].url == "https://example.com/news/1"
    assert items[0].published_at.endswith("Z")


def test_finnhub_client_limits_to_5_items(httpx_mock):
    httpx_mock.add_response(
        json=[
            {"headline": f"News {i}", "url": f"https://example.com/{i}", "datetime": 1720000000 + i}
            for i in range(10)
        ]
    )
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert len(items) == 5


def test_finnhub_client_returns_empty_on_http_error(httpx_mock):
    httpx_mock.add_response(status_code=500)
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert items == []


def test_finnhub_client_returns_empty_on_timeout(httpx_mock):
    import httpx
    httpx_mock.add_exception(httpx.TimeoutException("timeout"))
    client = FinnhubNewsClient(api_key="test-key")
    items = client.fetch_news("7203")
    assert items == []


def test_mock_client_satisfies_protocol():
    from typing import runtime_checkable, Protocol

    client = MockNewsClient()
    result = client.fetch_news("7203")
    assert isinstance(result, list)
