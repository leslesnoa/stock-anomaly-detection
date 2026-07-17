import pytest
from fastapi.testclient import TestClient
from app.main import app


@pytest.fixture(scope="module")
def client():
    # lifespan が実行される。FINNHUB_API_KEY 未設定 → MockNewsClient 使用
    with TestClient(app) as c:
        yield c


def test_analyze_success(client):
    response = client.post(
        "/analyze",
        json={
            "stock_code": "7203",
            "z_score": 3.2,
            "current_price": 3250.0,
            "prices": [3000.0 + i * 10 for i in range(30)],
        },
    )
    assert response.status_code == 200
    body = response.json()
    assert body["stock_code"] == "7203"
    assert "rsi" in body["indicators"]
    assert "macd" in body["indicators"]
    assert "bollinger" in body["indicators"]
    assert isinstance(body["news"], list)


def test_analyze_returns_rsi_float(client):
    response = client.post(
        "/analyze",
        json={
            "stock_code": "6758",
            "z_score": 2.8,
            "current_price": 5000.0,
            "prices": [5000.0 + i * 5 for i in range(30)],
        },
    )
    assert response.status_code == 200
    rsi = response.json()["indicators"]["rsi"]
    assert rsi is None or isinstance(rsi, float)


def test_analyze_too_few_prices_returns_422(client):
    response = client.post(
        "/analyze",
        json={
            "stock_code": "7203",
            "z_score": 3.2,
            "current_price": 3250.0,
            "prices": [3000.0] * 13,  # 13件 < 14件
        },
    )
    assert response.status_code == 422


def test_health_still_works(client):
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
