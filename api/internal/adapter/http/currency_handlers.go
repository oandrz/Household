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

// currencySymbols carries the few symbols worth showing. It is deliberately
// partial: an unknown code renders as the bare code, which is what
// currencyLabel in the frontend already did. This lives here rather than in the
// frontend so there is one list, served rather than duplicated.
var currencySymbols = map[string]string{
	"AUD": "A$", "BRL": "R$", "CAD": "C$", "CHF": "CHF", "CNY": "¥",
	"EUR": "€", "GBP": "£", "HKD": "HK$", "IDR": "Rp", "INR": "₹",
	"MYR": "RM", "NZD": "NZ$", "PHP": "₱", "SGD": "S$", "THB": "฿",
	"USD": "$", "VND": "₫", "ZAR": "R",
}

// handleListCurrencies serves the currencies a household may choose. It is
// public: the sign-up form fetches it before any session exists.
//
// Reads domain.SelectableCurrencies rather than filtering
// domain.ActiveCurrencies itself, so this list and what NewSignupBlueprint's
// validation accepts (domain.ParseSelectableCurrency) cannot drift apart --
// see SelectableCurrencies's doc comment for why only two-minor-unit
// currencies are offered at all, and for the note on what to delete when
// Money learns about minor units.
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
