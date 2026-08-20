from pathlib import Path
import tempfile

from rich.console import Console

from sleepyrouter.cli.commands import run_usage_command
from sleepyrouter.config import ConfigStore
from sleepyrouter.types import UsageLogEntry


def test_usage_command_empty() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        store.ensure_root()

        console = Console(record=True)
        run_usage_command(store=store, console=console)
        output = console.export_text()

        assert "사용 기록이 없어요" in output
        store.close()


def test_usage_command_with_logs() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        store.ensure_root()

        store.append_usage(
            UsageLogEntry(
                ts="2026-08-20T10:00:00Z",
                model="openai/gpt-4o",
                input_tokens=1000,
                output_tokens=250,
                success=True,
            )
        )
        store.append_usage(
            UsageLogEntry(
                ts="2026-08-20T10:05:00Z",
                model="openai/gpt-4o",
                input_tokens=500,
                output_tokens=100,
                success=False,
            )
        )
        store.append_usage(
            UsageLogEntry(
                ts="2026-08-20T10:10:00Z",
                model="deepseek-v4-pro",
                input_tokens=2000,
                output_tokens=800,
                success=True,
            )
        )

        console = Console(record=True)
        run_usage_command(store=store, console=console)
        output = console.export_text()

        assert "모델별 사용량" in output
        assert "openai/gpt-4o" in output
        assert "deepseek-v4-pro" in output
        assert "합계" in output
        store.close()


def test_usage_command_with_date_filter() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        store.ensure_root()

        store.append_usage(
            UsageLogEntry(
                ts="2026-08-19T10:00:00Z",
                model="openai/gpt-4o",
                input_tokens=1000,
                output_tokens=250,
                success=True,
            )
        )

        console = Console(record=True)
        run_usage_command(date="20260820", store=store, console=console)
        output = console.export_text()

        assert "사용 기록이 없어요 (날짜: 20260820)" in output
        store.close()
