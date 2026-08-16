from collections.abc import Generator
from pathlib import Path
import tempfile

from fastapi.testclient import TestClient
import pytest

from sleepyrouter.config import ConfigStore
from sleepyrouter.server import create_app
from sleepyrouter.types import ModelDefinition, SleepyRouterConfig


@pytest.fixture
def store_and_client() -> Generator[tuple[ConfigStore, TestClient], None, None]:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        store.ensure_root()
        env = {"OPENROUTER_API_KEY": "sk-test"}
        app = create_app(store=store, env=env)
        client = TestClient(app)
        yield store, client
        store.close()


def test_health_endpoint(store_and_client: tuple[ConfigStore, TestClient]) -> None:
    _, client = store_and_client
    res = client.get("/health")
    assert res.status_code == 200
    data = res.json()
    assert data["ok"] is True
    assert data["service"] == "sleepyrouter"


def test_models_endpoint_empty(
    store_and_client: tuple[ConfigStore, TestClient],
) -> None:
    _, client = store_and_client
    res = client.get("/v1/models")
    assert res.status_code == 200
    data = res.json()
    assert data["object"] == "list"
    assert data["data"] == []


def test_models_endpoint_with_config(
    store_and_client: tuple[ConfigStore, TestClient],
) -> None:
    store, client = store_and_client
    store.write_config(
        SleepyRouterConfig(
            port=4567,
            model_groups={"fast": ["fast-model"]},
            models={
                "fast-model": ModelDefinition(provider="openrouter", name="openai/gpt-4o-mini")
            },
        )
    )

    res = client.get("/v1/models")
    assert res.status_code == 200
    data = res.json()
    assert len(data["data"]) == 1
    assert data["data"][0]["id"] == "fast-model"
    assert data["data"][0]["owned_by"] == "openrouter"


def test_count_tokens_endpoint(
    store_and_client: tuple[ConfigStore, TestClient],
) -> None:
    _, client = store_and_client
    res = client.post(
        "/anthropic/v1/messages/count_tokens",
        json={"messages": [{"role": "user", "content": "Hello world"}]},
    )
    assert res.status_code == 200
    data = res.json()
    assert "input_tokens" in data
    assert data["input_tokens"] > 0


def test_chat_completions_missing_models(
    store_and_client: tuple[ConfigStore, TestClient],
) -> None:
    _, client = store_and_client
    res = client.post(
        "/v1/chat/completions",
        json={"messages": [{"role": "user", "content": "Hi"}]},
    )
    assert res.status_code == 400
    data = res.json()
    assert "선택된 무료 모델이 없어요" in data["error"]["message"]
