from fastapi import APIRouter, Depends
from app.dependencies import get_analysis_service
from app.schemas import AnalyzeRequest, AnalyzeResponse
from app.services.analysis import AnalysisService

router = APIRouter()


@router.post("/analyze", response_model=AnalyzeResponse)
def analyze(req: AnalyzeRequest, svc: AnalysisService = Depends(get_analysis_service)) -> AnalyzeResponse:
    return svc.analyze(req)
