from fastapi import FastAPI
from app.routers import analyze as analyze_router

app = FastAPI()
app.include_router(analyze_router.router)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}
