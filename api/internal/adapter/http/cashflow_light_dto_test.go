package httpadapter

import (
	"encoding/json"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestCashflowLightDTOSaysWhenADueBillCouldNotBeConverted pins the wire field
// the frontend needs to word an unconvertible bill honestly. It is a pure
// mapping, so it needs no container.
func TestCashflowLightDTOSaysWhenADueBillCouldNotBeConverted(t *testing.T) {
	v := domain.CashflowVerdict{Colour: domain.LightYellow, Reasons: []domain.LightReason{
		{Code: domain.ReasonBillDueSoon, BillName: "Foreign tax", BillUnconvertible: true},
		{Code: domain.ReasonBillDueSoon, BillName: "Car insurance"},
	}}
	raw, err := json.Marshal(toCashflowLightDTO(v))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Reasons []map[string]any `json:"reasons"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Reasons) != 2 {
		t.Fatalf("reasons = %v", got.Reasons)
	}
	if got.Reasons[0]["billUnconvertible"] != true || got.Reasons[1]["billUnconvertible"] != false {
		t.Fatalf("billUnconvertible = %v, %v; want true, false", got.Reasons[0]["billUnconvertible"], got.Reasons[1]["billUnconvertible"])
	}
}
