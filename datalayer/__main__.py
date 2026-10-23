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
import os
import signal
import sys
import tempfile

from .config_io import load_criteria
from .futuclient import OpenDError, QuoteClient
from .progress import Progress

# Futu's stock screen takes at most twelve consecutive years of a financial
# property; asking for more returns nothing at all.
MAX_CONTINUOUS_YEARS = 12


def prefilter(criteria: dict) -> tuple[float | None, int]:
    """The ROE floor and window the first pass screens on.

    It is the user's own floor, not a looser one of its own: the per-stock
    pass makes the real decision, so the prefilter only has to avoid
    dropping a stock the rules would keep.

    With no slack, every judged year must clear the floor, and the endpoint
    can be asked for exactly that. With slack allowed, a stock may miss any
    year in the window, which the endpoint's consecutive-year window cannot
    express — so there is no ROE condition it can state without risking a
    stock the rules would keep, and it states none.

    The window is capped because asking for more than twelve years matches
    nothing at all.
    """
    years = int(criteria["roe_years"])
    if int(criteria["roe_slack"]) > 0:
        return None, 0
    return float(criteria["roe_min"]), min(max(years, 1), MAX_CONTINUOUS_YEARS)


# Set when the app asks us to stop. The loop checks it between stocks, so a
# cancelled run ends at a stock boundary and still writes what it has.
_stopping = False


def _stop_on_signal(signum, frame) -> None:
    global _stopping
    _stopping = True


def _install_stop_handler() -> None:
    """Stop at the next stock when the app cancels, and keep what it has.

    The packet is rewritten after every stock, so even a hard kill leaves the
    finished ones on disk. The handler is what makes that a clean stop where
    the signal is delivered: the loop ends at a stock boundary.
    """
    for name in ("SIGTERM", "SIGINT", "SIGBREAK"):
        sig = getattr(signal, name, None)
        if sig is None:
            continue
        try:
            signal.signal(sig, _stop_on_signal)
        except (ValueError, OSError):
            # Not the main thread, or no such signal on this platform.
            pass


def main(argv: list[str] | None = None) -> int:
    _install_stop_handler()
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
    """Stocks whose live PE and recent ROE clear the prefilter.

    The prefilter is the user's own rule, loosened only as far as it must be:
    the per-stock pass makes the real decision, so any stock it would keep
    has to survive here too.
    """
    progress.phase("first pass")
    floor, years = prefilter(criteria)
    out = client.first_pass(
        criteria["markets"],
        criteria["pe_max"],
        floor,
        years,
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

    out: list[dict] = []
    save = _saver(args.out, criteria, out)
    for i, code in enumerate(codes, 1):
        if _stopping:
            break
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
        except Exception as exc:  # noqa: BLE001 - see below
            # One stock the SDK stumbled over must not cost the run every
            # stock after it: the entry carries the reason, as a refusal
            # does, and the window offers a retry.
            entry["error"] = f"{type(exc).__name__}: {exc}"
        out.append(entry)
        # Written as it goes: a cancelled run is killed outright on Windows,
        # so this file is all that survives it.
        save()
    progress.done(len(out))
    return out


def _saver(out: str | None, criteria: dict, stocks: list[dict]):
    """Return a function that writes the packet so far, atomically.

    Atomic, because the file is read after a hard kill: a half-written packet
    would read as no packet at all and lose the whole run.
    """
    if not out:
        return lambda: None

    def save() -> None:
        text = json.dumps({"criteria": criteria, "stocks": stocks},
                          ensure_ascii=False)
        folder = os.path.dirname(os.path.abspath(out))
        fd, tmp = tempfile.mkstemp(dir=folder, prefix=".packet-", suffix=".json")
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as fh:
                fh.write(text)
            os.replace(tmp, out)
        except BaseException:
            try:
                os.unlink(tmp)
            except OSError:
                pass
            raise

    return save


def _write(dataset: dict, out: str | None, progress: Progress) -> None:
    text = json.dumps(dataset, ensure_ascii=False)
    if out:
        with open(out, "w", encoding="utf-8") as fh:
            fh.write(text)
        return
    sys.stdout.write(text)


if __name__ == "__main__":
    raise SystemExit(main())
