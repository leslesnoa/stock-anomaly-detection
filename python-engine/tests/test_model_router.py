import pytest
from fastapi.testclient import TestClient

from app.main import app
from app.services.direction_model import direction_model_state


@pytest.fixture(scope="function")
def client():
    direction_model_state.reset()
    with TestClient(app) as c:
        yield c
    direction_model_state.reset()


def _train_payload(n_stocks: int = 3, n_days: int = 400) -> dict:
    import datetime

    start = datetime.date(2024, 1, 1)
    dates = [(start + datetime.timedelta(days=i)).isoformat() for i in range(n_days)]
    return {
        "stocks": [
            {
                "stock_code": f"TEST{i}",
                "prices": [
                    {"date": d, "close": 1000.0 * (1 + 0.005 * ((-1) ** i)) ** j}
                    for j, d in enumerate(dates)
                ],
            }
            for i in range(n_stocks)
        ]
    }


def test_model_train_success(client):
    response = client.post("/model/train", json=_train_payload())
    assert response.status_code == 200
    body = response.json()
    assert "adopted" in body
    assert "independent_sample_count" in body


def test_model_train_rejects_concurrent_calls(client):
    assert direction_model_state.try_begin_training() is True
    response = client.post("/model/train", json=_train_payload())
    assert response.status_code == 409
    direction_model_state.reset()


def test_model_status_before_training_is_untrained(client):
    response = client.get("/model/status")
    assert response.status_code == 200
    body = response.json()
    assert body["adopted"] is False
    assert body["trained_at"] is None
    assert body["recent_results"] == []


def test_model_status_after_training_reflects_result(client):
    client.post("/model/train", json=_train_payload())
    response = client.get("/model/status")
    body = response.json()
    assert body["trained_at"] is not None
