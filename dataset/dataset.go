// Package dataset reads the JSON the Python data layer produces and turns it
// into the types the engine judges.
package dataset

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/LuckyCurve/value-screener/engine"
)

// Raw is the on-disk shape written by `python -m datalayer`. Years arrive as
// string keys because JSON object keys are strings.
type Raw struct {
	Stocks []RawStock `json:"stocks"`
}

// RawStock is one candidate plus the figures fetched for it.
type RawStock struct {
	Code      string             `json:"code"`
	Name      string             `json:"name"`
	PE        float64            `json:"pe"`
	PB        float64            `json:"pb"`
	MarketCap float64            `json:"market_cap"`
	ROE       map[string]float64 `json:"roe"`
	Balance   map[string]RawBal  `json:"balance"`
	Valuation *engine.Valuation  `json:"valuation"`
	Error     string             `json:"error"`
}

// RawBal is one year's cash and debt.
type RawBal struct {
	Cash float64 `json:"cash"`
	Debt float64 `json:"debt"`
}

// Parse decodes a dataset and converts its year keys to ints.
func Parse(data []byte) ([]engine.Verdict, error) {
	var raw Raw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse dataset: %w", err)
	}

	out := make([]engine.Verdict, 0, len(raw.Stocks))
	for _, s := range raw.Stocks {
		roe := engine.ROEReport{ROE: make(map[int]float64, len(s.ROE))}
		for key, v := range s.ROE {
			year, err := strconv.Atoi(key)
			if err != nil {
				return nil, fmt.Errorf("stock %s: bad ROE year %q: %w", s.Code, key, err)
			}
			roe.ROE[year] = v
		}

		bal := engine.BalanceReport{Years: make(map[int]engine.Balance, len(s.Balance))}
		for key, b := range s.Balance {
			year, err := strconv.Atoi(key)
			if err != nil {
				return nil, fmt.Errorf("stock %s: bad balance year %q: %w", s.Code, key, err)
			}
			bal.Years[year] = engine.Balance{Cash: b.Cash, Debt: b.Debt}
		}

		out = append(out, engine.Verdict{
			Stock: engine.Stock{
				Code:      s.Code,
				Name:      s.Name,
				PE:        s.PE,
				PB:        s.PB,
				MarketCap: s.MarketCap,
			},
			ROE:       roe,
			Balance:   bal,
			Valuation: s.Valuation,
		})
	}
	return out, nil
}
