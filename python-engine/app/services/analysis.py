from app.schemas import AnalyzeRequest, AnalyzeResponse
from app.services.indicators import calculate_indicators
from app.services.news_client import NewsClient


class AnalysisService:
    def __init__(self, news_client: NewsClient) -> None:
        self._news_client = news_client

    def analyze(self, request: AnalyzeRequest) -> AnalyzeResponse:
        indicators = calculate_indicators(request.prices)
        news = self._news_client.fetch_news(request.stock_code)
        return AnalyzeResponse(
            stock_code=request.stock_code,
            indicators=indicators,
            news=news,
        )
