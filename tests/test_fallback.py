from collections.abc import AsyncGenerator
from pathlib import Path
import tempfile
from typing import Any
from unittest.mock import MagicMock, patch

from fastapi.testclient import TestClient

from sleepyrouter.config import ConfigStore
from sleepyrouter.server import create_app
from sleepyrouter.types import ModelDefinition, SleepyRouterConfig


def test_candidate_failover_all_fail() -> None:
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
                    "fail-model-1": ModelDefinition(provider="openrouter", name="fail-1"),
                    "fail-model-2": ModelDefinition(provider="openrouter", name="fail-2"),
                },
            )
        )

        app = create_app(store=store, env={"OPENROUTER_API_KEY": "sk-test"})
        client = TestClient(app)

        async def mock_acompletion(*args: Any, **kwargs: Any) -> Any:
            raise RuntimeError("Upstream 500 connection error")

        with patch(
            "openai.resources.chat.completions.AsyncCompletions.create",
            side_effect=mock_acompletion,
        ):
            res = client.post(
                "/v1/chat/completions",
                json={"model": "high", "messages": [{"role": "user", "content": "hi"}]},
            )
            assert res.status_code == 502
            data = res.json()
            assert data["error"]["message"] == "선택된 모든 무료 모델이 실패했어요."

        store.close()


def test_candidate_failover_success_on_second() -> None:
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

        attempted_models: list[str] = []

        class MockResponseObj:
            def model_dump(self) -> dict[str, Any]:
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

        async def mock_acompletion(*args: Any, **kwargs: Any) -> Any:
            model_param = str(kwargs.get("model", ""))
            attempted_models.append(model_param)
            if "m1" in model_param:
                raise RuntimeError("Rate limit exceeded 429")
            return MockResponseObj()

        with patch(
            "openai.resources.chat.completions.AsyncCompletions.create",
            side_effect=mock_acompletion,
        ):
            res = client.post(
                "/v1/chat/completions",
                json={"model": "high", "messages": [{"role": "user", "content": "hi"}]},
            )
            assert res.status_code == 200
            data = res.json()
            assert data["choices"][0]["message"]["content"] == "Success response!"
            assert attempted_models == ["m1", "m2"]

        store.close()


def test_candidate_stream_failover_success_on_second() -> None:
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

        captured_kwargs: list[dict[str, Any]] = []

        async def mock_stream_acompletion(*args: Any, **kwargs: Any) -> Any:
            captured_kwargs.append(kwargs)
            model_param = str(kwargs.get("model", ""))
            if "m1" in model_param:
                raise RuntimeError("Rate limit exceeded 429 on stream start")

            async def _stream_gen() -> AsyncGenerator[Any, None]:
                chunk = MagicMock()
                chunk.choices = [MagicMock()]
                chunk.choices[0].delta = MagicMock(content="Stream chunk from m2")
                chunk.model_dump.return_value = {
                    "choices": [{"delta": {"content": "Stream chunk from m2"}}]
                }
                chunk.usage = None
                yield chunk

            return _stream_gen()

        with patch(
            "openai.resources.chat.completions.AsyncCompletions.create",
            side_effect=mock_stream_acompletion,
        ):
            res = client.post(
                "/v1/chat/completions",
                json={
                    "model": "high",
                    "stream": True,
                    "messages": [{"role": "user", "content": "hi"}],
                },
            )
            assert res.status_code == 200
            assert "Stream chunk from m2" in res.text

        assert captured_kwargs[0]["stream_options"] == {"include_usage": True}

        store.close()


def test_stream_failover_when_first_candidate_yields_empty_stream() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        store.ensure_root()

        store.write_config(
            SleepyRouterConfig(
                port=4567,
                model_groups={"high": ["empty-model", "working-model"]},
                default_model_group="high",
                models={
                    "empty-model": ModelDefinition(provider="openrouter", name="m1"),
                    "working-model": ModelDefinition(provider="openrouter", name="m2"),
                },
            )
        )

        app = create_app(store=store, env={"OPENROUTER_API_KEY": "sk-test"})
        client = TestClient(app)

        async def mock_stream_acompletion(*args: Any, **kwargs: Any) -> Any:
            model_param = str(kwargs.get("model", ""))
            if "m1" in model_param:
                async def _empty_gen() -> AsyncGenerator[Any, None]:
                    if False:
                        yield None

                return _empty_gen()

            async def _working_gen() -> AsyncGenerator[Any, None]:
                chunk = MagicMock()
                chunk.choices = [MagicMock()]
                chunk.choices[0].delta = MagicMock(content="Working chunk from m2")
                chunk.model_dump.return_value = {
                    "choices": [{"delta": {"content": "Working chunk from m2"}}]
                }
                chunk.usage = None
                yield chunk

            return _working_gen()

        with patch(
            "openai.resources.chat.completions.AsyncCompletions.create",
            side_effect=mock_stream_acompletion,
        ):
            res = client.post(
                "/v1/chat/completions",
                json={
                    "model": "high",
                    "stream": True,
                    "messages": [{"role": "user", "content": "hi"}],
                },
            )
            assert res.status_code == 200
            assert "Working chunk from m2" in res.text

        store.close()
