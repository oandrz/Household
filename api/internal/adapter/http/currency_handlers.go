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
// Only two-minor-unit currencies are offered. domain.Money.String() is
// fmt.Sprintf("%s%s %d.%02d", ...) -- two decimal places, hard-coded -- so a
// household that picked JPY (0) or KWD (3) would have every amount rendered
// wrong. domain.ParseCurrency still accepts every active code, because the
// household PATCH path has always accepted arbitrary codes and must not start
// rejecting existing data; this filter is only about what we *offer*.
//
// WHEN Money LEARNS ABOUT MINOR UNITS, DELETE THIS FILTER. It is the only thing
// keeping the list short.
func handleListCurrencies() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		all := domain.ActiveCurrencies()
		out := make([]currencyDTO, 0, len(all))
		for _, c := range all {
			if c.MinorUnits != 2 {
				continue
			}
			out = append(out, currencyDTO{Code: c.Code, Symbol: currencySymbols[c.Code], Name: c.Name})
		}
		WriteJSON(w, http.StatusOK, map[string]any{"currencies": out})
	}
}
