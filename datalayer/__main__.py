"""Command-line entry point the Go app calls.

Reads a criteria file, fetches the figures from OpenD, and writes the raw
data as JSON on stdout. It makes no pass/fail decision: the Go engine owns
the rules, so the same numbers always produce the same verdict.

Usage:
    python -m datalayer --config criteria.json --out data.json
    python -m datalayer --config criteria.json --out data.json --limit 5
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
    parser.add_argument("--config", help="criteria JSON file")
    parser.add_argument("--out", help="write the dataset here instead of stdout")
    parser.add_argument("--limit", type=int, default=0,
                        help="stop after N candidates (debugging)")
    args = parser.parse_args(argv)

    criteria = load_criteria(args.config)
    progress = Progress(sys.stderr)

    client = QuoteClient()
    try:
        client.connect()
    except OpenDError as exc:
        progress.error(str(exc))
        return 2

    dataset = {"criteria": criteria, "stocks": []}
    try:
        progress.phase("first pass")
        candidates = client.first_pass(
            criteria["markets"],
            criteria["pe_max"],
            criteria["coarse_roe_min"],
            min(criteria["roe_years"], 12),
        )
        if args.limit:
            candidates = candidates[: args.limit]
        progress.found(len(candidates))

        for i, cand in enumerate(candidates, 1):
            code = cand["code"]
            progress.stock(i, len(candidates), code, cand.get("name", ""))
            entry = dict(cand)
            try:
                entry["roe"] = {str(y): v for y, v in
                                client.roe_series(code).items()}
                entry["balance"] = {
                    str(y): v for y, v in client.balance_series(code).items()
                }
                if criteria["percentile_years"]:
                    entry["valuation"] = client.valuation(
                        code, criteria["percentile_years"])
            except OpenDError as exc:
                entry["error"] = str(exc)
            dataset["stocks"].append(entry)
    finally:
        client.close()

    text = json.dumps(dataset, ensure_ascii=False)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as fh:
            fh.write(text)
        progress.done(len(dataset["stocks"]))
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
