// SCOPE:layer=feature,removal=feature — gogen-ui view helpers.
package genui

import (
	"math"
	"strconv"
	"strings"
)

// barValue prints a bar value without trailing noise (250, not 250.0).
func barValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// barWidth scales a value against the series peak as a CSS width.
// Defensive zero: the catalog rejects peak<=0, but a width must never
// render NaN if validation is ever bypassed.
func barWidth(v, peak float64) string {
	if peak <= 0 || v <= 0 {
		return "0%"
	}
	pct := math.Round(v/peak*1000) / 10
	return strconv.FormatFloat(pct, 'f', -1, 64) + "%"
}

// barClass cycles theme tokens so multi-bar charts stay distinguishable
// in both color modes without hard-coded hues.
func barClass(i int) string {
	classes := []string{"bg-primary", "bg-secondary", "bg-accent"}
	return classes[i%len(classes)]
}

// actionVerb maps a validated method to its Datastar action name.
func actionVerb(method string) string {
	return strings.ToLower(method)
}
