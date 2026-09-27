from fastapi import APIRouter
from app.schemas import ForecastRequest, ForecastResponse
from app.services.forecast import calculate_forecast

router = APIRouter()


@router.post("/forecast", response_model=ForecastResponse)
def forecast(req: ForecastRequest) -> ForecastResponse:
    return calculate_forecast(req.stock_code, req.current_price, req.prices)
