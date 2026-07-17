from pydantic import BaseModel, field_validator


class AnalyzeRequest(BaseModel):
    stock_code: str
    z_score: float
    current_price: float
    prices: list[float]

    @field_validator("prices")
    @classmethod
    def prices_min_length(cls, v: list[float]) -> list[float]:
        if len(v) < 14:
            raise ValueError("prices must have at least 14 elements")
        return v


class MACDValue(BaseModel):
    line: float | None
    signal: float | None
    histogram: float | None


class BollingerValue(BaseModel):
    upper: float | None
    middle: float | None
    lower: float | None


class Indicators(BaseModel):
    rsi: float | None
    macd: MACDValue
    bollinger: BollingerValue


class NewsItem(BaseModel):
    title: str
    url: str
    published_at: str


class AnalyzeResponse(BaseModel):
    stock_code: str
    indicators: Indicators
    news: list[NewsItem]
