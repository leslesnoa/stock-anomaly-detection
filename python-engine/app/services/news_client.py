from datetime import datetime, timedelta, timezone
from typing import Protocol

import httpx

from app.schemas import NewsItem

_FINNHUB_BASE_URL = "https://finnhub.io/api/v1/company-news"
_TIMEOUT = 5.0
_NEWS_LIMIT = 5


class NewsClient(Protocol):
    def fetch_news(self, stock_code: str) -> list[NewsItem]: ...


class FinnhubNewsClient:
    """
    Finnhub company-news エンドポイントを叩く本番実装。
    タイムアウト・HTTP エラー時は空リストを返す（呼び出し元で例外を出さない）。
    stock_code は 4桁コードを受け取り "TYO:{code}" に変換する。
    """

    def __init__(self, api_key: str) -> None:
        self._api_key = api_key

    def fetch_news(self, stock_code: str) -> list[NewsItem]:
        now = datetime.now(tz=timezone.utc)
        from_date = (now - timedelta(days=7)).strftime("%Y-%m-%d")
        to_date = now.strftime("%Y-%m-%d")

        try:
            with httpx.Client(timeout=_TIMEOUT) as client:
                resp = client.get(
                    _FINNHUB_BASE_URL,
                    params={
                        "symbol": f"TYO:{stock_code}",
                        "from": from_date,
                        "to": to_date,
                        "token": self._api_key,
                    },
                )
                resp.raise_for_status()
                raw_items: list[dict] = resp.json()
        except Exception:
            return []

        result: list[NewsItem] = []
        for item in raw_items[:_NEWS_LIMIT]:
            published_dt = datetime.fromtimestamp(item.get("datetime", 0), tz=timezone.utc)
            result.append(
                NewsItem(
                    title=item.get("headline", ""),
                    url=item.get("url", ""),
                    published_at=published_dt.strftime("%Y-%m-%dT%H:%M:%SZ"),
                )
            )
        return result


class MockNewsClient:
    """開発・テスト用。常に空リストを返す。"""

    def fetch_news(self, stock_code: str) -> list[NewsItem]:
        return []
