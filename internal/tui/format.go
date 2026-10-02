package tui

import (
	"fmt"
	"strings"
	"time"
)

var spanishMonths = [...]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}

// ago renders a past instant relative to now in Spanish: "hace 3 s",
// "hace 2 min", "hace 1 h", "ayer 18:40", or a date for older moments.
func ago(now, then time.Time) string {
	if then.IsZero() {
		return "nunca"
	}
	elapsed := now.Sub(then)
	switch {
	case elapsed < 0:
		return "ahora"
	case elapsed < time.Minute:
		return fmt.Sprintf("hace %d s", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("hace %d min", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour && sameDay(now, then):
		return fmt.Sprintf("hace %d h", int(elapsed.Hours()))
	case sameDay(now.AddDate(0, 0, -1), then):
		return "ayer " + then.Local().Format("15:04")
	default:
		return shortDate(then)
	}
}

func sameDay(a, b time.Time) bool {
	a, b = a.Local(), b.Local()
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// shortDate renders "28 sep" in the current year and "28 sep 2025" otherwise.
func shortDate(t time.Time) string {
	t = t.Local()
	date := fmt.Sprintf("%d %s", t.Day(), spanishMonths[t.Month()-1])
	if t.Year() != time.Now().Year() {
		date += fmt.Sprintf(" %d", t.Year())
	}
	return date
}

// duration renders a session length: "42 min", "1 h 10".
func duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "menos de 1 min"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d h %02d", int(d.Hours()), int(d.Minutes())%60)
	}
}

// thousands groups digits with a thin space the Spanish way: 10 000.
func thousands(n int) string {
	digits := fmt.Sprint(n)
	if len(digits) <= 3 {
		return digits
	}
	var out strings.Builder
	lead := len(digits) % 3
	if lead > 0 {
		out.WriteString(digits[:lead])
	}
	for index := lead; index < len(digits); index += 3 {
		if out.Len() > 0 {
			out.WriteString(" ")
		}
		out.WriteString(digits[index : index+3])
	}
	return out.String()
}

func version(v string) string {
	if v == "" {
		return "?"
	}
	if v == "dev" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// sessionLabel names a session by the short form of its handle.
func sessionLabel(handle string) string {
	short := strings.TrimPrefix(handle, "ps-")
	if len(short) > 8 {
		short = short[:8]
	}
	return "sesión " + short
}

// firstLine returns the first non-empty line of text.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%s %s", thousands(n), many)
}
