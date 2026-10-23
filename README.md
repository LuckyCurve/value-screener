# value-screener

A value-investing screen over Hong Kong, US and A-share listings, with a
native window: set the rules on the left, click **Run screen**, and the
stocks that clear every rule show up in a table. Click a row to see that
stock's figures year by year.

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

## Portable build

```sh
powershell -ExecutionPolicy Bypass -File scripts/build-windows.ps1
```

This makes `dist/value-screener/`, which runs on a machine with no Python and
no Go: the window, plus `value-screener-dl.exe`, the data layer frozen into a
single binary. Copy the folder anywhere, keep the two files together, and run
`value-screener.exe`. All it needs is OpenD running and logged in.

Run files (`criteria.run.json`, `dataset.json`) go to `data/` in that folder;
`criteria.json` beside the app holds the rules it opens on.

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
| `coarse_roe_min` | first-pass prefilter floor | `0.15` |
| `cash_years` | years the net-cash test looks at | `10` |
| `percentile_years` | PE history window: 1, 3, 5 or 10 | `10` |

## Layout

```
app/        the window: form, run, tables, year-by-year detail
engine/     the rules; the only place a pass or fail is decided
dataset/    reads the JSON the data layer writes
runner/     runs the data layer as a child process, reads its progress
config/     the criteria and their validation
datalayer/  Python: Futu OpenD → raw figures, no judgement
```

The split is deliberate. Python fetches; Go decides. The same numbers always
produce the same verdict, and the rules can be tested without a market being
open.

## Reading the numbers

A few traps the tables will not flag for you:

- ROE above 100% usually means equity is tiny, so the ratio says little.
- Low PE percentile on a stock that is always cheap is not a discount.
- Percentiles need a long history to mean anything; a stock listed two years
  ago has no ten-year percentile worth reading.
- Futu reports the figures in each market's own currency and unit.

## Licence

MIT.
