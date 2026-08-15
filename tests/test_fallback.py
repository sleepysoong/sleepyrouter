import tempfile
from pathlib import Path
from unittest.mock import patch

from fastapi.testclient import TestClient

from sleepyrouter.config import ConfigStore
from sleepyrouter.server import create_app
from sleepyrouter.types import ModelDefinition, SleepyRouterConfig


def test_candidate_failover_all_fail():
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        store.ensure_root()

        store.write_config(
            SleepyRouterConfig(
                port=4567,
                model_groups={"high": ["fail-model-1", "fail-model-2"]},
                default_model_group="high",
                models={
                    "fail-model-1": ModelDefinition(
                        provider="openrouter", name="fail-1"
                    ),
                    "fail-model-2": ModelDefinition(
                        provider="openrouter", name="fail-2"
                    ),
                },
            )
        )

        app = create_app(store=store, env={"OPENROUTER_API_KEY": "sk-test"})
        client = TestClient(app)

        # Mock acompletion to fail
        async def mock_acompletion(*args, **kwargs):
            raise RuntimeError("Upstream 500 connection error")

        with patch("sleepyrouter.server.acompletion", side_effect=mock_acompletion):
            res = client.post(
                "/v1/chat/completions",
                json={"model": "high", "messages": [{"role": "user", "content": "hi"}]},
            )
            assert res.status_code == 502
            data = res.json()
            assert data["error"]["message"] == "선택된 모든 무료 모델이 실패했어요."

        store.close()


def test_candidate_failover_success_on_second():
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        store.ensure_root()

        store.write_config(
            SleepyRouterConfig(
                port=4567,
                model_groups={"high": ["model-1", "model-2"]},
                default_model_group="high",
                models={
                    "model-1": ModelDefinition(provider="openrouter", name="m1"),
                    "model-2": ModelDefinition(provider="openrouter", name="m2"),
                },
            )
        )

        app = create_app(store=store, env={"OPENROUTER_API_KEY": "sk-test"})
        client = TestClient(app)

        attempted_models = []

        class MockResponseObj:
            def model_dump(self):
                return {
                    "id": "chatcmpl-success",
                    "object": "chat.completion",
                    "choices": [
                        {
                            "message": {
                                "role": "assistant",
                                "content": "Success response!",
                            },
                            "finish_reason": "stop",
                        }
                    ],
                    "usage": {"prompt_tokens": 10, "completion_tokens": 5},
                }

        async def mock_acompletion(*args, **kwargs):
            model_param = kwargs.get("model", "")
            attempted_models.append(model_param)
            if "m1" in model_param:
                raise RuntimeError("Rate limit exceeded 429")
            return MockResponseObj()

        with patch("sleepyrouter.server.acompletion", side_effect=mock_acompletion):
            res = client.post(
                "/v1/chat/completions",
                json={"model": "high", "messages": [{"role": "user", "content": "hi"}]},
            )
            assert res.status_code == 200
            data = res.json()
            assert data["choices"][0]["message"]["content"] == "Success response!"
            assert attempted_models == ["openrouter/m1", "openrouter/m2"]

        store.close()
