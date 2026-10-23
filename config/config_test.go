package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default criteria invalid: %v", err)
	}
}

func TestLoadMissingFileKeepsDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.PEmax != Default().PEmax || c.ROEYears != Default().ROEYears {
		t.Errorf("missing file should keep defaults, got %+v", c)
	}
}

func TestLoadOverridesOnlyNamedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	body := `{"pe_max": 15, "roe_min": 0.25}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.PEmax != 15 || c.ROEMin != 0.25 {
		t.Errorf("named fields not applied: %+v", c)
	}
	if c.ROEYears != Default().ROEYears || c.CashYears != Default().CashYears {
		t.Errorf("unset fields lost their defaults: %+v", c)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(path, []byte(`{"pe_max": `), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestValidateRejectsUnusableValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Criteria)
	}{
		{"zero pe", func(c *Criteria) { c.PEmax = 0 }},
		{"negative pe", func(c *Criteria) { c.PEmax = -1 }},
		{"zero roe", func(c *Criteria) { c.ROEMin = 0 }},
		{"no roe years", func(c *Criteria) { c.ROEYears = 0 }},
		{"slack equals years", func(c *Criteria) {
			c.ROESlack = c.ROEYears
		}},
		{"negative slack", func(c *Criteria) { c.ROESlack = -1 }},
		{"no cash years", func(c *Criteria) { c.CashYears = 0 }},
		{"no percentile years", func(c *Criteria) { c.PercentileYears = 0 }},
		{"no markets", func(c *Criteria) { c.Markets = nil }},
		{"unknown market", func(c *Criteria) { c.Markets = []string{"JP"} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			tc.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatalf("expected validation to fail for %+v", c)
			}
		})
	}
}

func TestValidateAllowsSlackBelowYears(t *testing.T) {
	c := Default()
	c.ROESlack = c.ROEYears - 1
	if err := c.Validate(); err != nil {
		t.Fatalf("slack of years-1 should be legal: %v", err)
	}
}

func TestMarshalRoundTrips(t *testing.T) {
	c := Default()
	c.PEmax = 12.5
	c.ROESlack = 2
	c.Markets = []string{"HK", "CN"}
	data, err := Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "criteria.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.PEmax != 12.5 || got.ROESlack != 2 || len(got.Markets) != 2 {
		t.Errorf("round trip lost values: %+v", got)
	}
}
