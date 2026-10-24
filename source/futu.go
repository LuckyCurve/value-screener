package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/LuckyCurve/value-screener/engine"
	"github.com/LuckyCurve/value-screener/runner"
)

// Futu reads from Futu's OpenD through the Python data layer. The Python
// side only fetches; everything here decides what to ask for and what to
// keep.
type Futu struct {
	// Dir is the project or app folder, where the data layer runs.
	Dir string
	// Binary, when set, is a self-contained data layer to run instead of
	// `python -m datalayer`.
	Binary string
	// Python overrides the interpreter.
	Python string
	// Temp is where the data layer writes its JSON.
	Temp string
}

// packetStock is one stock as the data layer writes it: in the packet, and
// in the streamed lines that arrive while the run is still going.
type packetStock struct {
	Code      string             `json:"code"`
	Name      string             `json:"name"`
	PE        float64            `json:"pe"`
	PB        float64            `json:"pb"`
	MarketCap float64            `json:"market_cap"`
	ROE       map[string]float64 `json:"roe"`
	Balance   map[string]struct {
		Cash float64 `json:"cash"`
		Debt float64 `json:"debt"`
	} `json:"balance"`
	Valuation *engine.Valuation `json:"valuation"`
	Error     string            `json:"error"`
}

// packet is the JSON the data layer writes.
type packet struct {
	Stocks []packetStock `json:"stocks"`
}

// Candidates runs the first pass. A cancelled run returns whatever the
// channel had written with the error, so the caller can tell a stop from a
// failure.
func (f Futu) Candidates(ctx context.Context, q Query, progress Progress) ([]Candidate, error) {
	return f.CandidatesStream(ctx, q, nil, progress)
}

// CandidatesStream is Candidates, reporting the list as soon as it is known
// rather than when the run ends.
func (f Futu) CandidatesStream(ctx context.Context, q Query, on func([]Candidate),
	progress Progress) ([]Candidate, error) {
	var out []Candidate
	data, err := f.run(ctx, "candidates", q, nil, 0, func(ev runner.Event) {
		if ev.Event != "candidate" || on == nil {
			return
		}
		var stocks []packetStock
		if json.Unmarshal(ev.Stocks, &stocks) != nil {
			return
		}
		out = fromCandidates(stocks)
		on(out)
	}, progress)
	if len(data) == 0 {
		return out, err
	}
	p, perr := decode(data)
	if perr != nil {
		return out, perr
	}
	// The packet is the authority: a stream line that was lost leaves the
	// same list here.
	out = fromCandidates(p.Stocks)
	return out, err
}

// Figures fetches the yearly reports and today's valuation for codes.
func (f Futu) Figures(ctx context.Context, q Query, codes []string, progress Progress) ([]Annual, error) {
	return f.FiguresStream(ctx, q, codes, nil, progress)
}

// FiguresStream is Figures, reporting each stock as it arrives: a screen of
// a few hundred stocks shows verdicts while it is still fetching.
//
// A cancelled fetch returns the stocks it reached with the error; they are
// judged like any other answer.
func (f Futu) FiguresStream(ctx context.Context, q Query, codes []string,
	on func(Annual), progress Progress) ([]Annual, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	var out []Annual
	data, err := f.run(ctx, "figures", q, codes, 0, func(ev runner.Event) {
		if ev.Event != "judged" || on == nil {
			return
		}
		var s packetStock
		if json.Unmarshal(ev.Stock, &s) != nil {
			return
		}
		a := fromAnnual(s)
		out = append(out, a)
		on(a)
	}, progress)

	// The packet is read again as the authority: it holds every stock the
	// channel finished, including those whose stream line was lost.
	if len(data) > 0 {
		if p, perr := decode(data); perr == nil {
			out = fromAnnuals(p.Stocks)
		}
	}
	return out, err
}

// fromCandidates maps a first pass's stocks as the data layer writes them.
func fromCandidates(stocks []packetStock) []Candidate {
	out := make([]Candidate, 0, len(stocks))
	for _, s := range stocks {
		out = append(out, Candidate{
			Code: s.Code, Name: s.Name, PE: s.PE, PB: s.PB, MarketCap: s.MarketCap,
		})
	}
	return out
}

func fromAnnuals(stocks []packetStock) []Annual {
	out := make([]Annual, 0, len(stocks))
	for _, s := range stocks {
		out = append(out, fromAnnual(s))
	}
	return out
}

// fromAnnual is one stock's report, with the year keys the packet writes as
// strings turned back into years.
func fromAnnual(s packetStock) Annual {
	a := Annual{
		Code:      s.Code,
		Name:      s.Name,
		ROE:       make(map[int]float64, len(s.ROE)),
		Balance:   make(map[int]Balance, len(s.Balance)),
		Valuation: s.Valuation,
		Error:     s.Error,
	}
	for k, v := range s.ROE {
		y, err := strconv.Atoi(k)
		if err != nil {
			a.Error = fmt.Sprintf("bad ROE year %q", k)
			continue
		}
		a.ROE[y] = v
	}
	for k, v := range s.Balance {
		y, err := strconv.Atoi(k)
		if err != nil {
			a.Error = fmt.Sprintf("bad balance year %q", k)
			continue
		}
		a.Balance[y] = Balance{Cash: v.Cash, Debt: v.Debt}
	}
	return a
}

func (f Futu) run(ctx context.Context, command string, q Query, codes []string,
	limit int, on func(runner.Event), progress Progress) ([]byte, error) {
	if err := os.MkdirAll(f.Temp, 0o755); err != nil {
		return nil, fmt.Errorf("prepare %s: %w", f.Temp, err)
	}
	cfgPath, err := f.writeQuery(q)
	if err != nil {
		return nil, err
	}
	outPath := filepath.Join(f.Temp, command+".json")

	args := []string{command, "--out", outPath, "--config", cfgPath}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
	if command == "figures" {
		// Codes go in a file, not the command line: a full-market list
		// would overflow it on Windows.
		codePath := filepath.Join(f.Temp, "codes.txt")
		var b []byte
		for _, c := range codes {
			b = append(b, c...)
			b = append(b, '\n')
		}
		if err := os.WriteFile(codePath, b, 0o644); err != nil {
			return nil, fmt.Errorf("write codes: %w", err)
		}
		args = append(args, "--code-file", codePath)
	}

	data, err := runner.Run(ctx, f.Dir, runner.Options{
		Python:    f.Python,
		DataLayer: f.Binary,
		Args:      args,
		OutPath:   outPath,
		OnEvent: func(ev runner.Event) {
			switch ev.Event {
			case "found":
				progress.report(0, ev.Count, "")
			case "stock":
				progress.report(ev.Index, ev.Total, ev.Code)
			case "error":
				progress.report(0, 0, ev.Message)
			}
			// The caller's own lines (the first pass, each judged stock) go
			// on to it; the rest are for the progress bar.
			on(ev)
		},
	})
	if errors.Is(err, runner.ErrUnreachable) {
		// Above this line a channel is unplugged, not broken, and the raw
		// log is no use to the person reading the window.
		return data, ErrUnavailable
	}
	return data, err
}

// writeQuery hands the criteria to the data layer, which needs the markets
// and the windows to know what to fetch.
func (f Futu) writeQuery(q Query) (string, error) {
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode criteria: %w", err)
	}
	path := filepath.Join(f.Temp, "criteria.run.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write criteria: %w", err)
	}
	return path, nil
}

func decode(data []byte) (packet, error) {
	var p packet
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("parse data layer output: %w", err)
	}
	return p, nil
}
