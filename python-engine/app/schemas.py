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


class DirectionModelInfo(BaseModel):
    adopted: bool
    predicted_direction: str | None
    hit_rate: float | None
    baseline_hit_rate: float | None
    p_value: float | None
    independent_sample_count: int | None
    trained_at: str | None


class ForecastResponse(BaseModel):
    stock_code: str
    horizon: int
    points: list[ForecastPoint]
    direction_model: DirectionModelInfo


class ModelTrainPricePoint(BaseModel):
    date: str
    close: float


class ModelTrainStock(BaseModel):
    stock_code: str
    prices: list[ModelTrainPricePoint]


class ModelTrainRequest(BaseModel):
    stocks: list[ModelTrainStock]


class ModelTrainResponse(BaseModel):
    adopted: bool
    hit_rate: float | None
    baseline_hit_rate: float | None
    p_value: float | None
    independent_sample_count: int | None
    trained_at: str | None


class ModelStatusResponse(BaseModel):
    adopted: bool
    hit_rate: float | None
    baseline_hit_rate: float | None
    p_value: float | None
    independent_sample_count: int | None
    trained_at: str | None
    recent_results: list[bool]
