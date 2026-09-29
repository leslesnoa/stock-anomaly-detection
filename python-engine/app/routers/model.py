from fastapi import APIRouter, HTTPException

from app.schemas import ModelStatusResponse, ModelTrainRequest, ModelTrainResponse
from app.services.direction_dataset import StockPriceSeries
from app.services.direction_model import direction_model_state

router = APIRouter()


@router.post("/model/train", response_model=ModelTrainResponse)
def train_model(req: ModelTrainRequest) -> ModelTrainResponse:
    if not direction_model_state.try_begin_training():
        raise HTTPException(status_code=409, detail="training already in progress")

    stocks = [
        StockPriceSeries(
            stock_code=s.stock_code,
            dates=[p.date for p in s.prices],
            prices=[p.close for p in s.prices],
        )
        for s in req.stocks
    ]
    status = direction_model_state.train(stocks)
    return ModelTrainResponse(
        adopted=status.adopted,
        hit_rate=status.hit_rate,
        baseline_hit_rate=status.baseline_hit_rate,
        p_value=status.p_value,
        independent_sample_count=status.independent_sample_count,
        trained_at=status.trained_at,
    )


@router.get("/model/status", response_model=ModelStatusResponse)
def model_status() -> ModelStatusResponse:
    status = direction_model_state.status()
    return ModelStatusResponse(
        adopted=status.adopted,
        hit_rate=status.hit_rate,
        baseline_hit_rate=status.baseline_hit_rate,
        p_value=status.p_value,
        independent_sample_count=status.independent_sample_count,
        trained_at=status.trained_at,
        recent_results=list(status.recent_results),
    )
