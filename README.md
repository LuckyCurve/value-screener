# value-screener

A value-investing screen over Hong Kong, US and A-share listings, with a
native window: set the rules on the left, click **Run screen**, and the
stocks that clear every rule show up in a table. Click a row to open that
stock's figures year by year. Stocks the channel could not answer for are a
group of their own, each with a retry.

The figures come from Futu's OpenD, which must be running and logged in on
`127.0.0.1:11111`. Nothing is traded: the app only opens a quote context.

## Rules

1. **Price** — trailing PE at or under the ceiling, positive.
2. **ROE** — clears the floor in every year of the window, or in all but the
   years the slack allows.
3. **Net cash** — cash exceeds interest-bearing debt in every year of the
   window.
4. **Valuation** — today's PE placed among the stock's own daily PE history;
   the percentile sorts the results, cheapest first.

The first pass asks OpenD for candidates by PE and by the same ROE floor, over
at most twelve years — the most its screen endpoint accepts. The per-stock pass
then applies the rules in full, including the slack. Cancelling a run stops the
fetch at the stock in flight and keeps the stocks already judged.

OpenD cannot answer the other three criteria from the usual checklist —
management integrity, a focused strategy, auditor and incentive design — so
they are not scored. The window shows what the data says and leaves those
calls to you.

## Setup

Requires Go 1.27+, Python 3.10+, and OpenD running and logged in.

```sh
python -m venv .venv
.venv/Scripts/pip install futu-api "protobuf<4"   # .venv/bin/pip on macOS and Linux
```

`protobuf<4` is not optional: `futu-api` needs the pre-4 runtime to build its
enum descriptors.

## Run

From source:

```sh
go run .
```

The window opens on `criteria.json` if there is one beside `datalayer/`, and
on the shipped defaults otherwise.

## Build

```sh
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1
```

This writes two files to `dist\`:

- `value-screener.exe` — one file, nothing to install. The data layer is
  packed inside it and unpacks itself into the user's cache the first time a
  run starts.
- `Value screen Setup 1.0.0 amd64.exe` — the same app as a per-user install,
  with a Start menu shortcut, an optional desktop shortcut, and an
  uninstaller under Settings > Apps.

Both need only OpenD running and logged in: no Python, no Go. Settings and
the cache live beside the executable, or in `%LOCALAPPDATA%\Programs` after
an install.

## Tests

CI runs these on every push and pull request
(`.github/workflows/test.yml`): Go on Linux and Windows with the race
detector, and the Python data layer on its own.

```sh
go test ./...              # Go: rules, cache policy, process wiring, the window
.venv/Scripts/python -m pytest datalayer   # the data layer
go test -tags live ./app -run Live -v      # against a running OpenD
```

The window is tested without a window: `ui.NewTester` builds each frame,
clicks by label, and reads back the text, so the tests exercise the same view
code the app runs. To look at that rendering yourself:

```sh
VALUE_SCREENER_SHOTS=out go test ./app -run TestShots
```

## Tests

CI runs these on every push and pull request
(`.github/workflows/test.yml`): Go on Linux and Windows with the race
detector, and the Python data layer on its own.

```sh
go test ./...              # Go: rules, parsing, process wiring, the window
.venv/Scripts/python -m pytest datalayer   # the data layer
go test -tags live ./app -run Live -v      # against a running OpenD
```

The window is tested without a window: `ui.NewTester` builds each frame,
clicks by label, and reads back the text, so the tests exercise the same view
code the app runs.

## Configuration

`criteria.json` is read at start-up and rewritten per run as
`criteria.run.json`, which is what the Python data layer is handed. Every
field the window edits lives here:

| Field | Meaning | Default |
| --- | --- | --- |
| `markets` | `HK`, `US`, `CN` | all three |
| `pe_max` | trailing PE ceiling | `20` |
| `roe_min` | ROE floor, as a fraction | `0.20` |
| `roe_years` | annual reports the ROE test looks at | `15` |
| `roe_slack` | how many of those may miss the floor | `0` |
| `cash_years` | years the net-cash test looks at | `10` |
| `percentile_years` | PE history window: 1, 3, 5 or 10 | `10` |

## Layout

See [AGENTS.md](AGENTS.md) for the design and the rules that keep the layers
apart. In short:

```
app/        the window: form, run, three groups of results, the dialog
screener/   cache policy and judging: who to ask for what, what to keep
source/     the channel: today Futu, behind two methods
cache/      SQLite: closed fiscal years and recorded refusals
runner/     runs the data layer as a child process, reads its progress
engine/     the rules; the only place a pass or fail is decided
config/     the criteria and their validation
datalayer/  Python: Futu OpenD -> raw figures, no judgement
```

The split is deliberate. Python fetches; Go decides, and remembers only what
cannot change. Swapping Futu for another channel is one file in `source/`.

## The cache

`value-screener-cache.db` beside the app holds the annual ROE and the cash
and debt of every closed balance sheet. A stock whose years are all there is
judged without asking Futu at all.

Anything that moves with the share price — PE, market capitalisation, the PE
percentile — is never cached and is fetched every run. A refusal from the
channel is remembered too, so the "No data" group says how recently it
happened; the retry button clears it and asks again.

## Reading the numbers

A few traps the tables will not flag for you:

- ROE above 100% usually means equity is tiny, so the ratio says little.
- Low PE percentile on a stock that is always cheap is not a discount.
- Percentiles need a long history to mean anything; a stock listed two years
  ago has no ten-year percentile worth reading.
- Futu reports the figures in each market's own currency and unit.

## Licence

MIT.
