from fastapi import APIRouter
from app.schemas import AnalyzeRequest, AnalyzeResponse
from app.services.indicators import calculate_indicators

router = APIRouter()


@router.post("/analyze", response_model=AnalyzeResponse)
def analyze(req: AnalyzeRequest) -> AnalyzeResponse:
    indicators = calculate_indicators(req.prices)
    return AnalyzeResponse(stock_code=req.stock_code, indicators=indicators)
