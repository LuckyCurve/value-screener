"""Command-line entry point the Go app calls.

The data layer is a channel: it fetches from OpenD and reports what it got,
nothing more. It holds no cache and makes no pass/fail decision. The app
decides which figures to ask for and remembers what will not change, so a
different channel — another broker, a filing archive — can be added behind
the same two commands without touching the rules or the cache.

Two commands, because they age differently:

    candidates  a screened first pass. PE is live, so this is never reused.
    figures     annual reports for named codes. Closed fiscal years never
                change, so the app caches these and asks only for what it
                is missing.

Usage:
    python -m datalayer candidates --config criteria.json --out out.json
    python -m datalayer figures --config criteria.json --out out.json --code HK.00700
"""

from __future__ import annotations

import argparse
import json
import sys

from .config_io import load_criteria
from .futuclient import OpenDError, QuoteClient
from .progress import Progress


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="datalayer")
    sub = parser.add_subparsers(dest="command", required=True)

    cand = sub.add_parser("candidates", help="screen for candidates")
    cand.add_argument("--config", help="criteria JSON file")
    cand.add_argument("--out", help="write here instead of stdout")
    cand.add_argument("--limit", type=int, default=0,
                      help="stop after N candidates (debugging)")

    fig = sub.add_parser("figures", help="fetch annual reports for codes")
    fig.add_argument("--config", help="criteria JSON file")
    fig.add_argument("--out", help="write here instead of stdout")
    fig.add_argument("--limit", type=int, default=0,
                     help="stop after N codes (debugging)")
    # Repeated rather than nargs="*", so a long retry list cannot overflow
    # a command line.
    fig.add_argument("--code", action="append", default=[],
                     help="a code to fetch; repeat for several")
    fig.add_argument("--code-file",
                     help="read codes from this file, one per line")

    args = parser.parse_args(argv)
    criteria = load_criteria(args.config)
    progress = Progress(sys.stderr)

    client = QuoteClient()
    try:
        client.connect()
    except OpenDError as exc:
        progress.error(str(exc))
        return 2

    try:
        if args.command == "candidates":
            stocks = _candidates(client, criteria, args, progress)
        else:
            stocks = _figures(client, criteria, args, progress)
    finally:
        client.close()

    _write({"criteria": criteria, "stocks": stocks}, args.out, progress)
    return 0


def _candidates(client: QuoteClient, criteria: dict, args, progress) -> list[dict]:
    """Stocks whose live PE and recent ROE clear the prefilter."""
    progress.phase("first pass")
    out = client.first_pass(
        criteria["markets"],
        criteria["pe_max"],
        criteria["coarse_roe_min"],
        min(criteria["roe_years"], 12),
    )
    if args.limit:
        out = out[: args.limit]
    progress.found(len(out))
    return out


def _figures(client: QuoteClient, criteria: dict, args, progress) -> list[dict]:
    """Annual reports and today's valuation for the codes named.

    The caller decides which codes still need fetching; this reports what
    Futu says, including its refusal, which the caller records so a later
    run need not ask again.
    """
    codes = list(args.code)
    if args.code_file:
        with open(args.code_file, encoding="utf-8") as fh:
            codes += [line.strip() for line in fh if line.strip()]
    if args.limit:
        codes = codes[: args.limit]

    out = []
    for i, code in enumerate(codes, 1):
        progress.stock(i, len(codes), code, "")
        entry: dict = {"code": code}
        try:
            entry["roe"] = {str(y): v for y, v in client.roe_series(code).items()}
            entry["balance"] = {
                str(y): v for y, v in client.balance_series(code).items()
            }
            if criteria["percentile_years"]:
                # Never cached by the caller: it moves with the share price.
                entry["valuation"] = client.valuation(
                    code, criteria["percentile_years"])
        except OpenDError as exc:
            entry["error"] = str(exc)
        out.append(entry)
    progress.done(len(out))
    return out


def _write(dataset: dict, out: str | None, progress: Progress) -> None:
    text = json.dumps(dataset, ensure_ascii=False)
    if out:
        with open(out, "w", encoding="utf-8") as fh:
            fh.write(text)
        return
    sys.stdout.write(text)


if __name__ == "__main__":
    raise SystemExit(main())
