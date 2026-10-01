// Package money converts between the decimal strings stored in files and
// integer pence. Arithmetic never touches a float.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Pence is an amount in the minor unit of its currency.
type Pence int64

// Parse reads "650", "650.5", "650.50" or "1,250.00". More than two decimal
// places is an error rather than a rounding.
func Parse(s string) (Pence, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if hasFrac && (len(frac) == 0 || len(frac) > 2) {
		return 0, fmt.Errorf("%q: use at most two decimal places", s)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an amount", s)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an amount", s)
	}
	p := Pence(w*100 + f)
	if neg {
		p = -p
	}
	return p, nil
}

// String renders the form stored in files: "650.00".
func (p Pence) String() string {
	sign := ""
	if p < 0 {
		sign, p = "-", -p
	}
	return fmt.Sprintf("%s%d.%02d", sign, p/100, p%100)
}

// Normalise parses and re-renders, so "650" is stored as "650.00".
func Normalise(s string) (string, error) {
	p, err := Parse(s)
	if err != nil {
		return "", err
	}
	return p.String(), nil
}
