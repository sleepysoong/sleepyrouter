"""CLI Argument parsing setup."""

import argparse

from sleepyrouter.server import VERSION


def build_cli_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="sleepyrouter")
    subparsers = parser.add_subparsers(dest="command")

    start_parser = subparsers.add_parser("start")
    start_parser.add_argument("--port", type=int, default=0, help="Port to serve on")

    usage_parser = subparsers.add_parser("usage")
    usage_parser.add_argument("--date", type=str, help="Date filter (YYYYMMDD)")
    usage_parser.add_argument("--week", type=int, help="Week filter")

    parser.add_argument("-v", "--version", action="version", version=VERSION)
    return parser
