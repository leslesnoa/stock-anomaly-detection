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


class AnalyzeResponse(BaseModel):
    stock_code: str
    indicators: Indicators


ESTIMATION_WINDOW = 120
FORECAST_HORIZON = 20


class ForecastRequest(BaseModel):
    stock_code: str
    current_price: float
    prices: list[float]

    @field_validator("prices")
    @classmethod
    def prices_min_length(cls, v: list[float]) -> list[float]:
        min_len = ESTIMATION_WINDOW + 1
        if len(v) < min_len:
            raise ValueError(f"prices must have at least {min_len} elements")
        return v


class ForecastPoint(BaseModel):
    step: int
    center: float
    upper_68: float
    lower_68: float
    upper_95: float
    lower_95: float


class ForecastResponse(BaseModel):
    stock_code: str
    horizon: int
    points: list[ForecastPoint]
