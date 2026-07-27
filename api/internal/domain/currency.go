package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Currency is one active ISO 4217 currency. MinorUnits is the number of
// decimal places the currency actually has: 2 for most, 0 for JPY and KRW, 3
// for the Gulf dinars. Money.String() hard-codes two, so MinorUnits is what
// lets a caller refuse to offer a currency the money path would render wrong.
type Currency struct {
	Code       string
	Name       string
	MinorUnits int
}

// activeCurrencies is the ISO 4217 active list. It is a package-level var
// rather than a function-local literal so ActiveCurrencies can sort it once,
// in init, instead of on every call.
//
// This is the single reference for "is this a currency" in this codebase:
// ParseCurrency below is the only validator, NewMoney delegates to it, and
// usecase.normalizeCurrency delegates to NewMoney. Do not add a second list
// anywhere -- the frontend deliberately reads GET /api/v1/currencies rather
// than keeping its own.
var activeCurrencies = []Currency{
	{"AED", "UAE dirham", 2}, {"AFN", "Afghan afghani", 2},
	{"ALL", "Albanian lek", 2}, {"AMD", "Armenian dram", 2},
	{"ANG", "Netherlands Antillean guilder", 2}, {"AOA", "Angolan kwanza", 2},
	{"ARS", "Argentine peso", 2}, {"AUD", "Australian dollar", 2},
	{"AWG", "Aruban florin", 2}, {"AZN", "Azerbaijani manat", 2},
	{"BAM", "Bosnia and Herzegovina convertible mark", 2},
	{"BBD", "Barbados dollar", 2}, {"BDT", "Bangladeshi taka", 2},
	{"BGN", "Bulgarian lev", 2}, {"BHD", "Bahraini dinar", 3},
	{"BIF", "Burundian franc", 0}, {"BMD", "Bermudian dollar", 2},
	{"BND", "Brunei dollar", 2}, {"BOB", "Boliviano", 2},
	{"BRL", "Brazilian real", 2}, {"BSD", "Bahamian dollar", 2},
	{"BTN", "Bhutanese ngultrum", 2}, {"BWP", "Botswana pula", 2},
	{"BYN", "Belarusian ruble", 2}, {"BZD", "Belize dollar", 2},
	{"CAD", "Canadian dollar", 2}, {"CDF", "Congolese franc", 2},
	{"CHF", "Swiss franc", 2}, {"CLP", "Chilean peso", 0},
	{"CNY", "Renminbi", 2}, {"COP", "Colombian peso", 2},
	{"CRC", "Costa Rican colon", 2}, {"CUP", "Cuban peso", 2},
	{"CVE", "Cape Verdean escudo", 2}, {"CZK", "Czech koruna", 2},
	{"DJF", "Djiboutian franc", 0}, {"DKK", "Danish krone", 2},
	{"DOP", "Dominican peso", 2}, {"DZD", "Algerian dinar", 2},
	{"EGP", "Egyptian pound", 2}, {"ERN", "Eritrean nakfa", 2},
	{"ETB", "Ethiopian birr", 2}, {"EUR", "Euro", 2},
	{"FJD", "Fiji dollar", 2}, {"FKP", "Falkland Islands pound", 2},
	{"GBP", "Pound sterling", 2}, {"GEL", "Georgian lari", 2},
	{"GHS", "Ghanaian cedi", 2}, {"GIP", "Gibraltar pound", 2},
	{"GMD", "Gambian dalasi", 2}, {"GNF", "Guinean franc", 0},
	{"GTQ", "Guatemalan quetzal", 2}, {"GYD", "Guyanese dollar", 2},
	{"HKD", "Hong Kong dollar", 2}, {"HNL", "Honduran lempira", 2},
	{"HTG", "Haitian gourde", 2}, {"HUF", "Hungarian forint", 2},
	{"IDR", "Indonesian rupiah", 2}, {"ILS", "Israeli new shekel", 2},
	{"INR", "Indian rupee", 2}, {"IQD", "Iraqi dinar", 3},
	{"IRR", "Iranian rial", 2}, {"ISK", "Icelandic krona", 0},
	{"JMD", "Jamaican dollar", 2}, {"JOD", "Jordanian dinar", 3},
	{"JPY", "Japanese yen", 0}, {"KES", "Kenyan shilling", 2},
	{"KGS", "Kyrgyzstani som", 2}, {"KHR", "Cambodian riel", 2},
	{"KMF", "Comoro franc", 0}, {"KPW", "North Korean won", 2},
	{"KRW", "South Korean won", 0}, {"KWD", "Kuwaiti dinar", 3},
	{"KYD", "Cayman Islands dollar", 2}, {"KZT", "Kazakhstani tenge", 2},
	{"LAK", "Lao kip", 2}, {"LBP", "Lebanese pound", 2},
	{"LKR", "Sri Lankan rupee", 2}, {"LRD", "Liberian dollar", 2},
	{"LSL", "Lesotho loti", 2}, {"LYD", "Libyan dinar", 3},
	{"MAD", "Moroccan dirham", 2}, {"MDL", "Moldovan leu", 2},
	{"MGA", "Malagasy ariary", 2}, {"MKD", "Macedonian denar", 2},
	{"MMK", "Myanmar kyat", 2}, {"MNT", "Mongolian togrog", 2},
	{"MOP", "Macanese pataca", 2}, {"MRU", "Mauritanian ouguiya", 2},
	{"MUR", "Mauritian rupee", 2}, {"MVR", "Maldivian rufiyaa", 2},
	{"MWK", "Malawian kwacha", 2}, {"MXN", "Mexican peso", 2},
	{"MYR", "Malaysian ringgit", 2}, {"MZN", "Mozambican metical", 2},
	{"NAD", "Namibian dollar", 2}, {"NGN", "Nigerian naira", 2},
	{"NIO", "Nicaraguan cordoba", 2}, {"NOK", "Norwegian krone", 2},
	{"NPR", "Nepalese rupee", 2}, {"NZD", "New Zealand dollar", 2},
	{"OMR", "Omani rial", 3}, {"PAB", "Panamanian balboa", 2},
	{"PEN", "Peruvian sol", 2}, {"PGK", "Papua New Guinean kina", 2},
	{"PHP", "Philippine peso", 2}, {"PKR", "Pakistani rupee", 2},
	{"PLN", "Polish zloty", 2}, {"PYG", "Paraguayan guarani", 0},
	{"QAR", "Qatari riyal", 2}, {"RON", "Romanian leu", 2},
	{"RSD", "Serbian dinar", 2}, {"RUB", "Russian ruble", 2},
	{"RWF", "Rwandan franc", 0}, {"SAR", "Saudi riyal", 2},
	{"SBD", "Solomon Islands dollar", 2}, {"SCR", "Seychelles rupee", 2},
	{"SDG", "Sudanese pound", 2}, {"SEK", "Swedish krona", 2},
	{"SGD", "Singapore dollar", 2}, {"SHP", "Saint Helena pound", 2},
	{"SLE", "Sierra Leonean leone", 2}, {"SOS", "Somalian shilling", 2},
	{"SRD", "Surinamese dollar", 2}, {"SSP", "South Sudanese pound", 2},
	{"STN", "Sao Tome and Principe dobra", 2}, {"SVC", "Salvadoran colon", 2},
	{"SYP", "Syrian pound", 2}, {"SZL", "Swazi lilangeni", 2},
	{"THB", "Thai baht", 2}, {"TJS", "Tajikistani somoni", 2},
	{"TMT", "Turkmenistan manat", 2}, {"TND", "Tunisian dinar", 3},
	{"TOP", "Tongan paanga", 2}, {"TRY", "Turkish lira", 2},
	{"TTD", "Trinidad and Tobago dollar", 2}, {"TWD", "New Taiwan dollar", 2},
	{"TZS", "Tanzanian shilling", 2}, {"UAH", "Ukrainian hryvnia", 2},
	{"UGX", "Ugandan shilling", 0}, {"USD", "United States dollar", 2},
	{"UYU", "Uruguayan peso", 2}, {"UZS", "Uzbekistani sum", 2},
	{"VED", "Venezuelan digital bolivar", 2}, {"VES", "Venezuelan bolivar soberano", 2},
	{"VND", "Vietnamese dong", 0}, {"VUV", "Vanuatu vatu", 0},
	{"WST", "Samoan tala", 2}, {"XAF", "Central African CFA franc", 0},
	{"XCD", "East Caribbean dollar", 2}, {"XOF", "West African CFA franc", 0},
	{"XPF", "CFP franc", 0}, {"YER", "Yemeni rial", 2},
	{"ZAR", "South African rand", 2}, {"ZMW", "Zambian kwacha", 2},
	{"ZWG", "Zimbabwe gold", 2},
}

// byCode indexes activeCurrencies for O(1) lookup. Built in init rather than
// lazily so there is no locking and no first-call cost on a request path.
var byCode = map[string]Currency{}

func init() {
	sort.Slice(activeCurrencies, func(i, j int) bool {
		return activeCurrencies[i].Code < activeCurrencies[j].Code
	})
	for _, c := range activeCurrencies {
		byCode[c.Code] = c
	}
}

// ParseCurrency uppercases a currency code and checks it against the active
// ISO 4217 list. It is the single validator: NewMoney delegates to it, and so
// (transitively) does every caller-supplied currency on the household PATCH
// path.
//
// The error wraps ErrInvalidMoney rather than introducing a new sentinel,
// because adapter/http/errors.go already maps that to 422 INVALID_CURRENCY
// with the copy "That currency code is not valid." -- exactly what an unknown
// code deserves. A new sentinel would need a new case for no gain.
func ParseCurrency(code string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if _, ok := byCode[upper]; !ok {
		return "", fmt.Errorf("%w: %q is not an active ISO 4217 currency", ErrInvalidMoney, code)
	}
	return upper, nil
}

// CurrencyMinorUnits reports how many decimal places a currency has. The bool
// is false for an unknown code, exactly as a map lookup would be.
func CurrencyMinorUnits(code string) (int, bool) {
	c, ok := byCode[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return 0, false
	}
	return c.MinorUnits, true
}

// ActiveCurrencies returns the whole active list, sorted by code. It copies,
// so a caller cannot mutate the package's own state -- GET /api/v1/currencies
// hands its result straight to a JSON encoder and a filter, and neither should
// be able to reorder or corrupt this list for every later request.
func ActiveCurrencies() []Currency {
	out := make([]Currency, len(activeCurrencies))
	copy(out, activeCurrencies)
	return out
}
