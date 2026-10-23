package source

import (
	"context"
	"encoding/json"
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

// packet is the JSON the data layer writes.
type packet struct {
	Stocks []struct {
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
	} `json:"stocks"`
}

// Candidates runs the first pass.
func (f Futu) Candidates(ctx context.Context, q Query, progress Progress) ([]Candidate, error) {
	data, err := f.run(ctx, "candidates", q, nil, 0, progress)
	if err != nil {
		return nil, err
	}
	p, err := decode(data)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(p.Stocks))
	for _, s := range p.Stocks {
		out = append(out, Candidate{
			Code: s.Code, Name: s.Name, PE: s.PE, PB: s.PB, MarketCap: s.MarketCap,
		})
	}
	return out, nil
}

// Figures fetches the yearly reports and today's valuation for codes.
func (f Futu) Figures(ctx context.Context, q Query, codes []string, progress Progress) ([]Annual, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	data, err := f.run(ctx, "figures", q, codes, 0, progress)
	if err != nil {
		return nil, err
	}
	p, err := decode(data)
	if err != nil {
		return nil, err
	}
	out := make([]Annual, 0, len(p.Stocks))
	for _, s := range p.Stocks {
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
				return nil, fmt.Errorf("%s: bad ROE year %q: %w", s.Code, k, err)
			}
			a.ROE[y] = v
		}
		for k, v := range s.Balance {
			y, err := strconv.Atoi(k)
			if err != nil {
				return nil, fmt.Errorf("%s: bad balance year %q: %w", s.Code, k, err)
			}
			a.Balance[y] = Balance{Cash: v.Cash, Debt: v.Debt}
		}
		out = append(out, a)
	}
	return out, nil
}

func (f Futu) run(ctx context.Context, command string, q Query, codes []string,
	limit int, progress Progress) ([]byte, error) {
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

	return runner.Run(ctx, f.Dir, runner.Options{
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
		},
	})
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
