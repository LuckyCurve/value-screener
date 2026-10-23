package dataset

import "testing"

func TestParseConvertsYearKeys(t *testing.T) {
	body := []byte(`{"stocks":[{
		"code":"HK.00700","name":"Tencent","pe":12.5,
		"roe":{"2023":0.25,"2024":0.28},
		"balance":{"2024":{"cash":100,"debt":40}},
		"valuation":{"percentile":5.5,"average":18,"current":12.5}
	}]}`)

	vs, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(vs))
	}
	v := vs[0]
	if v.Stock.Code != "HK.00700" || v.Stock.PE != 12.5 {
		t.Errorf("stock fields wrong: %+v", v.Stock)
	}
	if v.ROE.ROE[2024] != 0.28 || v.ROE.ROE[2023] != 0.25 {
		t.Errorf("roe not converted: %+v", v.ROE.ROE)
	}
	if b := v.Balance.Years[2024]; b.Cash != 100 || b.Debt != 40 {
		t.Errorf("balance not converted: %+v", b)
	}
	if v.Valuation == nil || v.Valuation.Percentile != 5.5 {
		t.Errorf("valuation missing: %+v", v.Valuation)
	}
}

func TestParseWithoutValuationLeavesNil(t *testing.T) {
	body := []byte(`{"stocks":[{"code":"X","roe":{},"balance":{}}]}`)
	vs, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if vs[0].Valuation != nil {
		t.Errorf("expected nil valuation, got %+v", vs[0].Valuation)
	}
}

func TestParseRejectsBadYearKey(t *testing.T) {
	body := []byte(`{"stocks":[{"code":"X","roe":{"FY24":0.2},"balance":{}}]}`)
	if _, err := Parse(body); err == nil {
		t.Fatal("expected an error for a non-numeric year")
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	if _, err := Parse([]byte(`{"stocks":`)); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestParseEmptyDataset(t *testing.T) {
	vs, err := Parse([]byte(`{"stocks":[]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("want no verdicts, got %d", len(vs))
	}
}

func TestParseKeepsPerStockError(t *testing.T) {
	body := []byte(`{"stocks":[{"code":"X","roe":{},"balance":{},"error":"OTC"}]}`)
	vs, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if vs[0].Stock.Code != "X" {
		t.Errorf("stock should still parse, got %+v", vs[0])
	}
}
