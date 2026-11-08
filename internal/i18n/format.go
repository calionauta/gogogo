package i18n

import (
	"context"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Style selects a date layout.
type Style int

const (
	StyleShort Style = iota
	StyleMedium
	StyleLong
)

// Num formats f with prec digits for the request locale via
// the standard printing package. An unknown tag prints with
// the default printer rather than failing the render.
func (c *Catalog) Num(ctx context.Context, f float64, prec int) string {
	if prec < 0 {
		prec = 0
	}
	if prec > 10 {
		prec = 10
	}
	printer := message.NewPrinter(tagFor(Locale(ctx)))
	return printer.Sprintf("%.*f", prec, f)
}

// Date formats t in the request locale. Full and long
// styles include the month name; short is numeric.
func (c *Catalog) Date(ctx context.Context, t time.Time, style Style) string {
	tag := Locale(ctx)
	switch style {
	case StyleLong, StyleMedium:
		return dateNamed(tag, t, style)
	default:
		return dateShort(tag, t)
	}
}

func tagFor(s string) language.Tag {
	tag, err := language.Parse(s)
	if err != nil {
		return language.English
	}
	return tag
}

func dateShort(tag string, t time.Time) string {
	if isPortuguese(tag) {
		return t.Format("02/01/2006")
	}
	return t.Format("01/02/2006")
}

func dateNamed(tag string, t time.Time, style Style) string {
	if isPortuguese(tag) {
		month := ptMonthShort[t.Month()]
		if style == StyleLong {
			month = ptMonthLong[t.Month()]
		}
		return strconv.Itoa(t.Day()) + " de " + month + " de " + strconv.Itoa(t.Year())
	}
	if style == StyleLong {
		return t.Format("January 2, 2006")
	}
	return t.Format("Jan 2, 2006")
}

func isPortuguese(tag string) bool {
	if tag == "" {
		return false
	}
	rest, _, _ := strings.Cut(tag, "-")
	base, _, _ := strings.Cut(rest, "_")
	return strings.EqualFold(base, "pt")
}

var ptMonthShort = map[time.Month]string{
	time.January:   "jan.",
	time.February:  "fev.",
	time.March:     "mar.",
	time.April:     "abr.",
	time.May:       "mai.",
	time.June:      "jun.",
	time.July:      "jul.",
	time.August:    "ago.",
	time.September: "set.",
	time.October:   "out.",
	time.November:  "nov.",
	time.December:  "dez.",
}

var ptMonthLong = map[time.Month]string{
	time.January:   "janeiro",
	time.February:  "fevereiro",
	time.March:     "março",
	time.April:     "abril",
	time.May:       "maio",
	time.June:      "junho",
	time.July:      "julho",
	time.August:    "agosto",
	time.September: "setembro",
	time.October:   "outubro",
	time.November:  "novembro",
	time.December:  "dezembro",
}
