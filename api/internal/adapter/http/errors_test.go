package httpadapter

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// Every row must answer something: the generic logged 500, or a full status,
// code and message. A row missing its message would answer an envelope with
// an empty sentence in it.
func TestEveryErrorRowAnswersSomething(t *testing.T) {
	for i, row := range domainErrorResponses {
		if len(row.sentinels) == 0 {
			t.Fatalf("row %d has no sentinels", i)
		}
		if row.internal {
			continue
		}
		if row.status == 0 || row.code == "" || row.message == "" {
			t.Fatalf("row %d (%v) is missing its status, code or message", i, row.sentinels)
		}
	}
}

// A sentinel listed twice can only ever reach its first row: the second is
// dead code that reads as if it did something.
func TestNoSentinelHasTwoRows(t *testing.T) {
	seen := map[error]int{}
	for i, row := range domainErrorResponses {
		for _, sentinel := range row.sentinels {
			if first, ok := seen[sentinel]; ok {
				t.Fatalf("%v is in row %d and again in row %d; only the first can ever match", sentinel, first, i)
			}
			seen[sentinel] = i
		}
	}
}

// The table's order is load-bearing (domainErrorResponses' own comment).
// These pin the places it matters today, so moving a row above a more
// specific one fails here rather than in a household's error message.
func TestErrorRowOrderPicksTheSpecificAnswer(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode string
	}{
		{
			name:     "a named conflict wins over the generic ALREADY_EXISTS",
			err:      errors.Join(domain.ErrCategoryNameTaken, domain.ErrAlreadyExists),
			wantCode: "CATEGORY_NAME_TAKEN",
		},
		{
			name:     "a sentinel wrapped by a repository still finds its row",
			err:      fmt.Errorf("create goal: constraint %q: %w", "goals_household_id_name_key", domain.ErrGoalNameTaken),
			wantCode: "GOAL_NAME_TAKEN",
		},
		{
			name:     "a not-payable bill unwraps to the forbidden row",
			err:      &domain.BillNotPayableError{Reason: domain.BillArchived},
			wantCode: "FORBIDDEN",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			MapDomainError(rec, httptest.NewRequest(http.MethodGet, "/", nil), tc.err)
			if !strings.Contains(rec.Body.String(), `"code":"`+tc.wantCode+`"`) {
				t.Fatalf("body = %s, want code %s", rec.Body.String(), tc.wantCode)
			}
		})
	}
}
