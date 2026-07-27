import os
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.routers import analyze as analyze_router
from app.services.analysis import AnalysisService
from app.services.news_client import FinnhubNewsClient, MockNewsClient


@asynccontextmanager
async def lifespan(app: FastAPI):
    api_key = os.getenv("FINNHUB_API_KEY")
    news_client = FinnhubNewsClient(api_key=api_key) if api_key else MockNewsClient()
    app.state.analysis_service = AnalysisService(news_client=news_client)
    yield


app = FastAPI(lifespan=lifespan)
app.include_router(analyze_router.router)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}
