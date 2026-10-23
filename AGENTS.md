# value-screener

A value-investing screen over HK, US and A-share listings. A native Go window
(mygo) reads the figures from a quote data channel, caches what cannot change,
and decides pass or fail against the rules in `config`.

Nothing here trades. The channel is a quote context only.

## Layers

Each layer knows only the one below it. Code that reaches across a layer is a
bug even when it compiles.

```
app/        the window: form, run, results            (mygo/ui)
  ↓
screener/   cache policy + judging: who to ask for what
  ↓              ↓
source/     the channel                cache/    SQLite, closed years only
  ↓
runner/     launches the data layer as a child process
  ↓
datalayer/  Python + Futu OpenAPI: fetch and report, decide nothing
```

| Package | Owns | Must not |
| --- | --- | --- |
| `app` | Window state, widgets, run lifecycle, retry | Judge, cache, name Futu |
| `screener` | Which years the cache is missing, judging, remembering | Know a vendor or a protocol |
| `source` | Turning a query into channel calls and a packet back | Cache, judge |
| `cache` | Closed fiscal years and recorded refusals | Hold anything price-derived |
| `runner` | Starting the child, progress lines, its last words | Know what it computes |
| `engine` | The rules, `Verdict`, `Result`, summaries | Fetch, cache, draw |
| `config` | The tunable thresholds and their JSON file | Anything else |

## The channel is pluggable

`source.Source` is the seam. Two methods, no vendor types:

```go
type Source interface {
	Candidates(ctx, Query, Progress) ([]Candidate, error) // codes, names, live PE
	Figures(ctx, Query, codes, Progress) ([]Annual, error) // yearly reports, refusal
}
```

`source.Futu` is today's only implementation; it happens to shell out to a
Python data layer. A new channel — an HTTP provider, a CSV replay, another
broker — is one file implementing those two methods, and nothing above it
changes. Keep it that way:

- No package outside `source/futu.go` may import `runner`, name Futu, or know
  that `datalayer/` exists.
- `screener`, `engine` and `app` take a `source.Source`; tests pass a fake.
- The data layer reports facts. It has no rules, no thresholds, no cache. If a
  decision needs a `config.Criteria` field, it belongs above `source`.

Swapping the channel must not require touching the cache schema or a test in
`screener`.

## The cache rule

The cache holds only what a past fiscal year fixed: annual ROE and the cash and
debt of closed balance sheets. Anything that moves with the share price is
fetched every run — PE, market capitalisation, the PE percentile, and the
average PE the percentile is measured against.

`screener/` enforces this by construction: `remember` writes only `Annual.ROE`
and `Annual.Balance`, and `fromCache` refuses to answer when the criteria ask
for a percentile. `cache/cache_test.go` pins the schema itself, so a future
column cannot quietly reintroduce a price.

A refusal ("not covered", "OTC quotes unavailable") is also a fact worth
keeping, in `failures`, so a retry can clear it.

A stored fact is only worth keeping if the convention behind it is right.
`cache.schemaVersion` is bumped when a convention turns out to be wrong (once:
Hong Kong debt was negated, making the net-cash test pass every HK stock), and
the migration drops that table so the values are fetched again.

Balance figures are magnitudes, in `futuclient.balance_series`. A statement's
own sign is Futu's convention — Hong Kong reports a borrowing positive, the US
labels it with a minus — and a rule that depends on it would pass one exchange
and fail another.

## Stopping

Cancel must stop the work, not merely stop waiting for it. A frozen data layer
is a bootloader that re-execs the real program, so `runner` puts the child in a
job object (Windows) or its own process group (Unix) and kills the tree:
killing only the process Go started would leave the one holding the OpenD
connection running to the end.

The result survives the stop. The data layer writes the packet after every
stock, `runner` reads it back with `ctx.Err()`, `screener` returns the verdicts
it reached, and `app` shows them with a "stopped early" note. Anything that
throws away a partial result — `return engine.Result{}, err` on any error — is
a bug: a stop after forty stocks must show forty.

## Adding a channel

1. Write the type in `source/`, implementing `Source`.
2. Map nothing: return `Candidate`, `Annual`, `Balance` as they are. Refusals go
   in `Annual.Error`, not in a Go `error` — a `error` stops the whole run.
3. Point `app.DefaultScreen()` at it. The cache and the rules are unchanged.

## Working on it

```sh
go test ./...          # everything; -race for the run goroutine
go vet ./...
.venv/Scripts/python -m pytest datalayer   # the data layer alone
VALUE_SCREENER_SHOTS=out go test ./app -run TestShots  # render the frames
```

`python scripts/make-icon.py` redraws `app/icon.png`, which is both the
executable's icon (mygo.json) and the window's (main.go).

Tests must not reach OpenD. `app` injects its `Screen` and `Retry`;
`screener` injects a fake `Source`; the live check in `app/live_test.go` is
behind the `live` build tag.

Build with `powershell -File scripts\build-windows.ps1`: it freezes the data
layer into `app/datalayer.bin`, builds with `-tags datalayer_embedded` (mygo
build reads it from GOFLAGS), and writes one runnable exe plus an NSIS
installer to `dist/`. A source build embeds nothing and runs
`python -m datalayer` from the project instead.

Requires Go 1.27+, and Python 3.10+ only to run or freeze the data layer.
`futu-api` needs `protobuf<4`.
