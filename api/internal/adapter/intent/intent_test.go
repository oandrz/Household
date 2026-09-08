package intent

import (
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestReadArgumentsFailsClosedOnAKindTheSchemaDoesNotName(t *testing.T) {
	got, err := ReadArguments([]byte(`{"kind":"transfer","amount":"5","description":"x","account":"","category":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "none" || got.Amount != "" {
		t.Fatalf("an invented kind must read as none with nothing else, got %+v", got)
	}
}

func TestReadArgumentsTrimsAndKeepsAKnownKind(t *testing.T) {
	got, err := ReadArguments([]byte(`{"kind":"income","amount":" 6500 ","description":" salary ","account":"DBS","category":""}`))
	if err != nil {
		t.Fatal(err)
	}
	want := usecase.Intent{Kind: "income", Amount: "6500", Description: "salary", Account: "DBS"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestReadArgumentsReportsMalformedJSONAsAnError(t *testing.T) {
	if _, err := ReadArguments([]byte(`{"kind":`)); err == nil {
		t.Fatal("malformed arguments must be an error, not a silent none")
	}
}

func TestSystemPromptCarriesTheNamesVerbatim(t *testing.T) {
	p := SystemPrompt(usecase.ParseIntentInput{Accounts: []string{"DBS Savings", "OCBC"}, Categories: []string{"Dining out"}})
	for _, s := range []string{"Accounts: DBS Savings; OCBC", "Categories: Dining out", ToolName} {
		if !strings.Contains(p, s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
}

func TestEveryPropertyIsRequired(t *testing.T) {
	props := Properties()
	for _, r := range Required() {
		if _, ok := props[r]; !ok {
			t.Errorf("required %q has no property", r)
		}
	}
	if len(props) != len(Required()) {
		t.Fatalf("%d properties but %d required: a field the model may omit is a field the reader must guess", len(props), len(Required()))
	}
}
