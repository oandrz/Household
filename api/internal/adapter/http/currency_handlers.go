package httpadapter

import (
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type currencyDTO struct {
	Code   string `json:"code"`
	Symbol string `json:"symbol,omitempty"`
	Name   string `json:"name"`
}

// currencySymbols is deliberately partial -- an unknown code renders as
// the bare code, matching what currencyLabel did in the frontend. It lives
// here, not there, so there is one list served rather than duplicated.
var currencySymbols = map[string]string{
	"AUD": "A$", "BRL": "R$", "CAD": "C$", "CHF": "CHF", "CNY": "¥",
	"EUR": "€", "GBP": "£", "HKD": "HK$", "IDR": "Rp", "INR": "₹",
	"MYR": "RM", "NZD": "NZ$", "PHP": "₱", "SGD": "S$", "THB": "฿",
	"USD": "$", "VND": "₫", "ZAR": "R",
}

// handleListCurrencies serves the currencies a household may choose. It is
// public because the sign-up form fetches it before any session exists.
//
// It reads domain.SelectableCurrencies rather than filtering
// domain.ActiveCurrencies itself, so this list and what
// NewSignupBlueprint's validation accepts (domain.ParseSelectableCurrency)
// cannot drift apart. See SelectableCurrencies's doc comment for the
// two-minor-unit rule and the minor-units TODO.
func handleListCurrencies() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		selectable := domain.SelectableCurrencies()
		out := make([]currencyDTO, 0, len(selectable))
		for _, c := range selectable {
			out = append(out, currencyDTO{Code: c.Code, Symbol: currencySymbols[c.Code], Name: c.Name})
		}
		WriteJSON(w, http.StatusOK, map[string]any{"currencies": out})
	}
}
