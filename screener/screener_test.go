package screener

import (
	"context"
	"errors"
	"testing"

	"github.com/LuckyCurve/value-screener/cache"
	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/LuckyCurve/value-screener/source"
)

// fake is a channel that records what it was asked for.
type fake struct {
	candidates []source.Candidate
	figures    map[string]source.Annual
	asked      []string
	screenErr  error
	figuresErr error
}

func (f *fake) Candidates(context.Context, source.Query, source.Progress) ([]source.Candidate, error) {
	if f.screenErr != nil {
		return nil, f.screenErr
	}
	return f.candidates, nil
}

func (f *fake) Figures(_ context.Context, _ source.Query, codes []string, _ source.Progress) ([]source.Annual, error) {
	if f.figuresErr != nil {
		return nil, f.figuresErr
	}
	f.asked = append(f.asked, codes...)
	out := make([]source.Annual, 0, len(codes))
	for _, c := range codes {
		if a, ok := f.figures[c]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

func criteria() config.Criteria {
	c := config.Default()
	c.Markets = []string{"HK"}
	c.PercentileYears = 0 // no live figure needed, so the cache can answer
	return c
}

func tencent() (source.Candidate, source.Annual) {
	c := source.Candidate{Code: "HK.00700", Name: "Tencent", PE: 13}
	a := source.Annual{Code: "HK.00700", Name: "Tencent"}
	a.ROE = map[int]float64{}
	a.Balance = map[int]source.Balance{}
	for y := 2024; y > 2024-15; y-- {
		a.ROE[y] = 0.25
	}
	for y := 2024; y > 2024-10; y-- {
		a.Balance[y] = source.Balance{Cash: 5e10, Debt: 1e10}
	}
	return c, a
}

func TestRunJudgesWhatTheChannelReturns(t *testing.T) {
	c, a := tencent()
	f := &fake{candidates: []source.Candidate{c},
		figures: map[string]source.Annual{c.Code: a}}
	res, err := Screen{Criteria: criteria(), Source: f}.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Passed) != 1 {
		t.Fatalf("want one pass, got %+v", res)
	}
	if res.Passed[0].Stock.Name != "Tencent" {
		t.Errorf("name lost: %+v", res.Passed[0].Stock)
	}
}

func TestClosedYearsAreCachedAndReused(t *testing.T) {
	c, a := tencent()
	store := cache.OpenMemory()
	defer store.Close()
	f := &fake{candidates: []source.Candidate{c},
		figures: map[string]source.Annual{c.Code: a}}

	s := Screen{Criteria: criteria(), Source: f, Cache: store}
	if _, err := s.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	cached, _ := store.ROE(c.Code)
	if len(cached) != 15 {
		t.Fatalf("want 15 cached years, got %d", len(cached))
	}

	// A second run must not ask the channel again.
	f.asked = nil
	res, err := s.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.asked) != 0 {
		t.Errorf("the cache should have answered, but the channel was asked for %v", f.asked)
	}
	if len(res.Passed) != 1 {
		t.Errorf("a cached stock should still be judged: %+v", res)
	}
}

func TestShortCacheIsRefetched(t *testing.T) {
	c, a := tencent()
	store := cache.OpenMemory()
	defer store.Close()
	_ = store.SaveROE(c.Code, map[int]float64{2024: 0.25}) // one year only

	f := &fake{candidates: []source.Candidate{c},
		figures: map[string]source.Annual{c.Code: a}}
	s := Screen{Criteria: criteria(), Source: f, Cache: store}
	if _, err := s.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(f.asked) != 1 {
		t.Errorf("a short cache must be refetched, asked %v", f.asked)
	}
	cached, _ := store.ROE(c.Code)
	if len(cached) != 15 {
		t.Errorf("the fetch should have filled the cache, got %d years", len(cached))
	}
}

func TestAValuationAlwaysGoesToTheChannel(t *testing.T) {
	c, a := tencent()
	a.Valuation = &engine.Valuation{Percentile: 5, Current: 13}
	store := cache.OpenMemory()
	defer store.Close()
	f := &fake{candidates: []source.Candidate{c},
		figures: map[string]source.Annual{c.Code: a}}

	cfg := criteria()
	cfg.PercentileYears = 10
	s := Screen{Criteria: cfg, Source: f, Cache: store}
	if _, err := s.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	// Fill the cache, then run again: the percentile is live, so the
	// channel is still asked.
	f.asked = nil
	if _, err := s.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(f.asked) != 1 {
		t.Errorf("a live percentile means asking every run, asked %v", f.asked)
	}
}

func TestTheValuationIsNeverCached(t *testing.T) {
	c, a := tencent()
	a.Valuation = &engine.Valuation{Percentile: 5, Current: 13}
	store := cache.OpenMemory()
	defer store.Close()
	f := &fake{candidates: []source.Candidate{c},
		figures: map[string]source.Annual{c.Code: a}}

	cfg := criteria()
	cfg.PercentileYears = 10
	if _, err := (Screen{Criteria: cfg, Source: f, Cache: store}).
		Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	// The cache has years, and nothing that came from the share price.
	roe, _ := store.ROE(c.Code)
	if len(roe) == 0 {
		t.Fatal("closed years should be cached")
	}
	// A cached year is a small decimal; a PE would not be.
	for y, v := range roe {
		if v > 2 {
			t.Errorf("year %d holds %v, which is not an ROE", y, v)
		}
	}
}

func TestRefusalIsReportedNotDropped(t *testing.T) {
	c, _ := tencent()
	f := &fake{candidates: []source.Candidate{c},
		figures: map[string]source.Annual{
			c.Code: {Code: c.Code, Error: "暂不提供美股 OTC 市场行情"},
		}}
	res, err := Screen{Criteria: criteria(), Source: f}.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Missing) != 1 {
		t.Fatalf("want one missing stock, got %+v", res)
	}
	if res.Missing[0].Unavailable == "" {
		t.Error("the refusal should be shown, not reported as a failed year")
	}
}

func TestRefusalIsRememberedAndCanBeForgotten(t *testing.T) {
	c, a := tencent()
	store := cache.OpenMemory()
	defer store.Close()
	f := &fake{candidates: []source.Candidate{c},
		figures: map[string]source.Annual{
			c.Code: {Code: c.Code, Error: "not covered"},
		}}
	s := Screen{Criteria: criteria(), Source: f, Cache: store}
	if _, err := s.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	reason, at, _ := store.Failure(c.Code)
	if reason == "" || at == "" {
		t.Fatalf("a refusal should be recorded, got %q at %q", reason, at)
	}

	// A retry clears it and fetches again.
	f.figures[c.Code] = a
	if _, err := s.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if reason, _, _ := store.Failure(c.Code); reason != "" {
		t.Errorf("a successful fetch should forget the failure, got %q", reason)
	}
}

func TestSilentChannelIsReported(t *testing.T) {
	c, _ := tencent()
	f := &fake{candidates: []source.Candidate{c}, figures: map[string]source.Annual{}}
	res, err := Screen{Criteria: criteria(), Source: f}.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Missing) != 1 || res.Missing[0].Unavailable == "" {
		t.Errorf("a candidate with no answer must be shown, got %+v", res.Missing)
	}
}

// A channel that stopped part-way answers for some stocks. Those answers are
// as true as any other; only the stocks never asked about are left out.
func TestAStoppedRunKeepsTheVerdictsItReached(t *testing.T) {
	first, a := tencent()
	second := source.Candidate{Code: "HK.00005", Name: "HSBC", PE: 9}
	f := &partial{candidates: []source.Candidate{first, second},
		figures: []source.Annual{a}, err: context.Canceled}

	res, err := Screen{Criteria: criteria(), Source: f}.Run(context.Background(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the stop should be reported, got %v", err)
	}
	if len(res.Passed) != 1 || res.Passed[0].Stock.Code != "HK.00700" {
		t.Fatalf("the stock it reached should be judged, got %+v", res)
	}
	if len(res.Missing) != 0 {
		t.Errorf("a stock never asked about must not be reported as refused: %+v", res.Missing)
	}
}

// partial is a channel that answers for some stocks and then stops.
type partial struct {
	candidates []source.Candidate
	figures    []source.Annual
	err        error
}

func (p *partial) Candidates(context.Context, source.Query, source.Progress) ([]source.Candidate, error) {
	return p.candidates, nil
}

func (p *partial) Figures(context.Context, source.Query, []string, source.Progress) ([]source.Annual, error) {
	return p.figures, p.err
}

func TestChannelErrorStopsTheRun(t *testing.T) {
	f := &fake{screenErr: errors.New("OpenD is not running")}
	_, err := Screen{Criteria: criteria(), Source: f}.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("a channel that cannot be reached should fail the run")
	}
}

func TestNoCacheStillWorks(t *testing.T) {
	c, a := tencent()
	f := &fake{candidates: []source.Candidate{c},
		figures: map[string]source.Annual{c.Code: a}}
	res, err := Screen{Criteria: criteria(), Source: f}.Run(context.Background(), nil)
	if err != nil || len(res.Passed) != 1 {
		t.Fatalf("a screen without a cache must still judge: %v %+v", err, res)
	}
}

func TestEmptyChannelIsAnEmptyResult(t *testing.T) {
	res, err := Screen{Criteria: criteria(), Source: &fake{}}.
		Run(context.Background(), nil)
	if err != nil || len(res.Passed)+len(res.Rejected)+len(res.Missing) != 0 {
		t.Errorf("got %+v %v", res, err)
	}
}
