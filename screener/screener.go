// Package screener turns a channel of figures into judged verdicts, using the
// cache for what cannot change.
//
// It sits between the source and the engine: it asks the source only for the
// years the cache is missing, remembers what it gets, and hands everything to
// the engine. A channel that is swapped out leaves the cache, the rules and
// the window untouched.
package screener

import (
	"context"
	"time"

	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/LuckyCurve/value-screener/source"
)

// Screen is what a run needs: the rules, the channel, the cache.
type Screen struct {
	Criteria config.Criteria
	Source   source.Source
	Cache    Store
}

// Store is the part of the cache the screener uses. It is an interface so a
// run can go without one.
type Store interface {
	ROE(code string) (map[int]float64, error)
	SaveROE(code string, years map[int]float64) error
	Balance(code string) (map[int]source.Balance, error)
	SaveBalance(code string, years map[int]source.Balance) error
	Failure(code string) (reason, at string, err error)
	PutFailure(code, reason, at string) error
	ClearFailure(code string) error
}

// Outstanding is a stock the channel could not answer for.
type Outstanding struct {
	Code   string
	Name   string
	Reason string
}

// Run fetches, caches and judges. It reports progress as it goes so the
// window can show which stock is in flight.
//
// A run the channel could not finish still returns the verdicts it reached,
// with the error: a stop after forty stocks should show forty, not nothing.
func (s Screen) Run(ctx context.Context, progress source.Progress) (engine.Result, error) {
	all, err := s.Source.Candidates(ctx, s.Query(), progress)
	if len(all) == 0 {
		return engine.BuildResult(s.Criteria, nil), err
	}

	verdicts := make([]engine.Verdict, 0, len(all))
	var need []source.Candidate
	var known []engine.Verdict

	for _, c := range all {
		v, ok := s.fromCache(c)
		if ok {
			known = append(known, v)
			continue
		}
		need = append(need, c)
	}

	// One fetch for everything the cache could not answer.
	fetched := []source.Annual{}
	if len(need) > 0 {
		codes := make([]string, len(need))
		for i, c := range need {
			codes[i] = c.Code
		}
		fetched, err = s.Source.Figures(ctx, s.Query(), codes, progress)
	}

	// The first pass already carries live PE and the name; the figures
	// call answers only for what is annual.
	cands := map[string]source.Candidate{}
	for _, c := range need {
		cands[c.Code] = c
	}

	// Candidates the channel returned without figures are reported rather
	// than silently dropped: a refusal is a fact worth showing.
	got := map[string]bool{}
	for _, a := range fetched {
		got[a.Code] = true
		v := s.judge(a, cands[a.Code])
		s.remember(a)
		if a.Error != "" {
			s.rememberFailure(a.Code, a.Error)
		} else {
			s.clearFailure(a.Code)
		}
		verdicts = append(verdicts, v)
	}
	// A run that stopped part-way never asked about the rest, so they are
	// not "no data": they are simply not in this report.
	if err == nil {
		for _, c := range need {
			if !got[c.Code] {
				verdicts = append(verdicts, engine.Verdict{
					Stock:       toStock(c),
					Unavailable: "the channel returned no figures",
				})
			}
		}
	}

	verdicts = append(verdicts, known...)
	return engine.BuildResult(s.Criteria, verdicts), err
}

// Retry asks the channel again for one stock the run could not answer for.
//
// The recorded refusal is cleared first, so a channel that has since come
// round is not filtered out by an old record. A refusal that repeats is
// written back and reported in the verdict, not returned as an error: only a
// channel that cannot be reached at all fails the retry.
func (s Screen) Retry(ctx context.Context, stock engine.Stock) (engine.Verdict, error) {
	s.clearFailure(stock.Code)
	fetched, err := s.Source.Figures(ctx, s.Query(), []string{stock.Code}, nil)
	if err != nil {
		return engine.Verdict{}, err
	}
	c := source.Candidate{Code: stock.Code, Name: stock.Name,
		PE: stock.PE, PB: stock.PB, MarketCap: stock.MarketCap}
	for _, a := range fetched {
		if a.Code != stock.Code {
			continue
		}
		v := s.judge(a, c)
		s.remember(a)
		if a.Error != "" {
			s.rememberFailure(a.Code, a.Error)
		}
		return v, nil
	}
	v := engine.Verdict{Stock: stock,
		Unavailable: "the channel returned no figures"}
	s.rememberFailure(stock.Code, v.Unavailable)
	return v, nil
}

// Query converts the rules into what the channel needs to know.
func (s Screen) Query() source.Query {
	c := s.Criteria
	return source.Query{
		Markets:         c.Markets,
		PEmax:           c.PEmax,
		ROEMin:          c.ROEMin,
		ROEYears:        c.ROEYears,
		ROESlack:        c.ROESlack,
		CashYears:       c.CashYears,
		PercentileYears: c.PercentileYears,
	}
}

// fromCache judges a candidate whose closed years are all cached.
//
// The valuation is never cached, so it cannot answer here: a stock with a
// point-in-time check outstanding always goes to the channel. Without that
// check there is nothing live to ask for, and the cache answers alone.
func (s Screen) fromCache(c source.Candidate) (engine.Verdict, bool) {
	if s.Cache == nil || s.Criteria.PercentileYears > 0 {
		return engine.Verdict{}, false
	}
	roe, err := s.Cache.ROE(c.Code)
	if err != nil {
		return engine.Verdict{}, false
	}
	bal, err := s.Cache.Balance(c.Code)
	if err != nil {
		return engine.Verdict{}, false
	}
	if !source.CachedYears(roe, s.Criteria.ROEYears) ||
		len(bal) < s.Criteria.CashYears {
		return engine.Verdict{}, false
	}
	reason, _, _ := s.Cache.Failure(c.Code)
	if reason != "" {
		return engine.Verdict{}, false
	}
	v := engine.EvaluateData(toStock(c), engine.ROEReport{ROE: roe},
		toReport(bal), s.Criteria, "")
	return v, true
}

// judge turns one channel answer into a verdict, keeping the live figures
// the first pass found.
func (s Screen) judge(a source.Annual, c source.Candidate) engine.Verdict {
	stock := toStock(c)
	stock.Code = a.Code
	if stock.Name == "" {
		stock.Name = a.Name
	}
	v := engine.EvaluateData(stock, engine.ROEReport{ROE: a.ROE},
		toReport(a.Balance), s.Criteria, a.Error)
	v.Valuation = a.Valuation
	return v
}

// remember stores the years that will not change. The valuation is left out
// on purpose: it follows the share price.
func (s Screen) remember(a source.Annual) {
	if s.Cache == nil || a.Error != "" {
		return
	}
	_ = s.Cache.SaveROE(a.Code, a.ROE)
	_ = s.Cache.SaveBalance(a.Code, a.Balance)
}

func (s Screen) rememberFailure(code, reason string) {
	if s.Cache == nil {
		return
	}
	_ = s.Cache.PutFailure(code, reason, Now())
}

func (s Screen) clearFailure(code string) {
	if s.Cache == nil {
		return
	}
	_ = s.Cache.ClearFailure(code)
}

func toStock(c source.Candidate) engine.Stock {
	return engine.Stock{Code: c.Code, Name: c.Name, PE: c.PE,
		PB: c.PB, MarketCap: c.MarketCap}
}

func toReport(b map[int]source.Balance) engine.BalanceReport {
	out := engine.BalanceReport{Years: make(map[int]engine.Balance, len(b))}
	for y, v := range b {
		out.Years[y] = engine.Balance{Cash: v.Cash, Debt: v.Debt}
	}
	return out
}

// Now is the time a failure was recorded, in UTC, for the window to show how
// old its record is.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }
