import pytest
from fastapi.testclient import TestClient
from app.main import app
from app.schemas import ESTIMATION_WINDOW


@pytest.fixture(scope="module")
def client():
    with TestClient(app) as c:
        yield c


def test_forecast_success(client):
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]
    response = client.post(
        "/forecast",
        json={"stock_code": "7203", "current_price": prices[-1], "prices": prices},
    )
    assert response.status_code == 200
    body = response.json()
    assert body["stock_code"] == "7203"
    assert body["horizon"] == 20
    assert len(body["points"]) == 20
    assert body["points"][0]["step"] == 1


def test_forecast_too_few_prices_returns_422(client):
    response = client.post(
        "/forecast",
        json={
            "stock_code": "7203",
            "current_price": 3000.0,
            "prices": [3000.0] * ESTIMATION_WINDOW,
        },
    )
    assert response.status_code == 422


def test_health_still_works(client):
    response = client.get("/health")
    assert response.status_code == 200


def test_forecast_response_always_includes_direction_model(client):
    prices = [100.0 * (1.01**i) for i in range(ESTIMATION_WINDOW + 1)]
    response = client.post(
        "/forecast",
        json={"stock_code": "7203", "current_price": prices[-1], "prices": prices},
    )
    body = response.json()
    assert "direction_model" in body
    assert body["direction_model"]["adopted"] is False
    assert body["direction_model"]["predicted_direction"] is None
