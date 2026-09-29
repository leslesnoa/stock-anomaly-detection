from fastapi import FastAPI
from app.routers import analyze as analyze_router
from app.routers import forecast as forecast_router
from app.routers import model as model_router

app = FastAPI()
app.include_router(analyze_router.router)
app.include_router(forecast_router.router)
app.include_router(model_router.router)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}
