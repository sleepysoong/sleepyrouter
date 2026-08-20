"""CLI commands implementation (start and usage)."""

import os
import sys
from typing import Any

from rich.console import Console
from rich.table import Table
import uvicorn

from sleepyrouter.config import (
    DEFAULT_PORT,
    ConfigStore,
    require_any_provider_api_key,
    resolve_provider_api_keys,
)
from sleepyrouter.routing import all_group_model_ids
from sleepyrouter.server import VERSION, create_app
from sleepyrouter.types import UsageLogEntry
from sleepyrouter.utils import get_config_path, get_env_path

from .parser import build_cli_parser


def _format_status(*, active: bool) -> str:
    return "✓" if active else "✗"


def _check_undefined_aliases(config: Any) -> list[str]:
    undefined_aliases: list[str] = []
    for group in sorted(config.model_groups.keys()):
        undefined_aliases.extend(
            alias
            for alias in config.model_groups.get(group, [])
            if config.models and alias not in config.models
        )
    return undefined_aliases


def run_start_command(port: int = 0, store: ConfigStore | None = None) -> None:
    store = store or ConfigStore()
    store.ensure_root()
    config = store.read_config()

    effective_port = port or config.port or DEFAULT_PORT
    if config.port != effective_port:
        config.port = effective_port
        store.write_config(config)

    env = dict(os.environ)
    keys = resolve_provider_api_keys(env, store.root)

    print(f"\nsleepyrouter v{VERSION}")
    print(f"  config: {get_config_path(store.root)}")
    print(f"  env: {get_env_path(store.root)}")
    print(f"  NVIDIA_API_KEY: {_format_status(active=bool(keys.nvidia))}")
    print(f"  OPENROUTER_API_KEY: {_format_status(active=bool(keys.open_router))}")
    print(f"  OPENCODE_API_KEY: {_format_status(active=bool(keys.zen))}")
    print(f"  GOOGLE_API_KEY: {_format_status(active=bool(keys.google))}")
    print(f"  FREEBUFF_API_KEY: {_format_status(active=bool(keys.freebuff))}")
    print(f"  ANTIGRAVITY_API_KEY: {_format_status(active=bool(keys.antigravity))}")

    require_any_provider_api_key(env, store.root)

    undefined = _check_undefined_aliases(config)
    if undefined:
        lines = [f"  - {m}" for m in undefined]
        msg = (
            "\n모델 그룹에 정의되지 않은 alias가 있어요. config.json의 models에 추가하세요:\n"
            + "\n".join(lines)
            + ": config.json을 수정한 후 다시 시도하세요"
        )
        raise ValueError(msg)

    group_names = sorted(config.model_groups.keys())
    if group_names:
        total_models = len(all_group_model_ids(config.model_groups, *config.group_order))
        print(f"\n모델 그룹 ({total_models}개 모델, {len(group_names)}개 그룹)")
        for name in group_names:
            marker = " (기본)" if name == config.default_model_group else ""
            print(f"  {name}{marker}: {', '.join(config.model_groups.get(name, []))}")
        if config.default_model_group:
            print(f"\n기본 그룹: {config.default_model_group}")
        print()

    app = create_app(store=store, env=env)
    print(f"sleepyrouter 서빙 시작: http://127.0.0.1:{effective_port}")
    print("종료하려면 Ctrl+C를 누르세요.\n")

    log_config = uvicorn.config.LOGGING_CONFIG.copy()
    if "loggers" in log_config and "uvicorn.access" in log_config["loggers"]:
        log_config["loggers"]["uvicorn.access"]["handlers"] = []
        log_config["loggers"]["uvicorn.access"]["propagate"] = False

    uvicorn.run(
        app,
        host="127.0.0.1",
        port=effective_port,
        log_level="warning",
        log_config=log_config,
        access_log=False,
        timeout_keep_alive=30,
    )


def _filter_logs_by_date(logs: list[UsageLogEntry], date: str) -> list[UsageLogEntry]:
    filtered: list[UsageLogEntry] = []
    for entry in logs:
        try:
            ymd = entry.ts.replace("-", "")[:8]
            if ymd == date:
                filtered.append(entry)
        except (ValueError, TypeError, KeyError):
            pass
    return filtered


def _aggregate_usage_rows(logs: list[UsageLogEntry]) -> list[dict[str, Any]]:
    by_model: dict[str, dict[str, Any]] = {}
    for entry in logs:
        if entry.model not in by_model:
            by_model[entry.model] = {
                "model": entry.model,
                "requests": 0,
                "failed": 0,
                "input_tokens": 0,
                "output_tokens": 0,
            }
        row = by_model[entry.model]
        row["requests"] += 1
        if not entry.success:
            row["failed"] += 1
        row["input_tokens"] += entry.input_tokens
        row["output_tokens"] += entry.output_tokens

    return sorted(
        by_model.values(),
        key=lambda r: (-r["requests"], -r["input_tokens"], r["model"]),
    )


def run_usage_command(
    date: str | None = None,
    week: int | None = None,
    store: ConfigStore | None = None,
    console: Console | None = None,
) -> None:
    active_store = store or ConfigStore()
    active_console = console or Console()
    logs = active_store.read_usage_logs()

    if date:
        logs = _filter_logs_by_date(logs, date)

    if not logs:
        filter_desc = f" (날짜: {date})" if date else (f" (주차: {week}주차)" if week else "")
        active_console.print(f"사용 기록이 없어요{filter_desc}.")
        return

    rows = _aggregate_usage_rows(logs)

    total_requests = sum(r["requests"] for r in rows)
    total_failed = sum(r["failed"] for r in rows)
    total_input = sum(r["input_tokens"] for r in rows)
    total_output = sum(r["output_tokens"] for r in rows)

    table = Table(title="모델별 사용량", show_footer=True, header_style="bold cyan")
    table.add_column("모델", style="white", footer="합계")
    table.add_column("요청", justify="right", style="cyan", footer=f"{total_requests:,}")
    table.add_column(
        "실패",
        justify="right",
        style="red" if total_failed > 0 else "green",
        footer=f"{total_failed:,}",
    )
    table.add_column("입력 토큰", justify="right", style="magenta", footer=f"{total_input:,}")
    table.add_column("출력 토큰", justify="right", style="green", footer=f"{total_output:,}")

    for r in rows:
        table.add_row(
            r["model"],
            f"{r['requests']:,}",
            f"{r['failed']:,}",
            f"{r['input_tokens']:,}",
            f"{r['output_tokens']:,}",
        )

    active_console.print()
    active_console.print(table)
    active_console.print()


def main() -> None:
    parser = build_cli_parser()
    args = parser.parse_args()

    if args.command == "start":
        try:
            run_start_command(port=args.port)
        except (ValueError, RuntimeError) as e:
            print(str(e), file=sys.stderr)
            sys.exit(1)
    elif args.command == "usage":
        run_usage_command(date=args.date, week=args.week)
    else:
        parser.print_help()
