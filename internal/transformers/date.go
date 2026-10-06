package transformers

import "time"

const (
	// DefaultDays is the default half-width of the date shift window.
	DefaultDays = 365
	// MaxDays bounds Options.Days.
	MaxDays = 36500

	dateLayout = "2006-01-02"
)

var (
	spaceLayouts = []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999-07",
	}
	tLayouts = []string{
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
	}
)

// DateShift moves a date or timestamp by a deterministic offset within
// ±Days (default 365), keeping the input's layout and time zone. Dates move
// by whole days, timestamps by whole seconds. PostgreSQL's "infinity" and
// "-infinity" are returned unchanged. Text that is not a recognizable date
// yields a deterministic date between 1970 and 2024.
func DateShift(in *Input) (string, error) {
	days := in.Opts.Days
	if days <= 0 {
		days = DefaultDays
	}
	if in.Text == "infinity" || in.Text == "-infinity" {
		return in.Text, nil
	}

	t, layout, ok := parseTime(in.Text)
	if !ok {
		return dateFallback(in), nil
	}

	var out time.Time
	if layout == dateLayout {
		off := int(in.S.Intn(uint64(2*days+1))) - days
		out = t.AddDate(0, 0, off)
		if !validYear(out) {
			out = t.AddDate(0, 0, -off)
		}
	} else {
		span := int64(days) * 86400
		off := time.Duration(int64(in.S.Intn(uint64(2*span+1)))-span) * time.Second
		out = t.Add(off)
		if !validYear(out) {
			out = t.Add(-off)
		}
	}
	return out.Format(layout), nil
}

func validYear(t time.Time) bool { return t.Year() >= 1 && t.Year() <= 9999 }

func parseTime(s string) (time.Time, string, bool) {
	var layouts []string
	switch {
	case len(s) == len(dateLayout):
		t, err := time.Parse(dateLayout, s)
		return t, dateLayout, err == nil
	case len(s) > 10 && s[10] == 'T':
		layouts = tLayouts
	case len(s) > 10 && s[10] == ' ':
		layouts = spaceLayouts
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, l, true
		}
	}
	return time.Time{}, "", false
}

func dateFallback(in *Input) string {
	t := time.Unix(0, 0).UTC().AddDate(0, 0, int(in.S.Intn(20000)))
	switch in.Kind {
	case Timestamp:
		return t.Format("2006-01-02 15:04:05")
	case TimestampTZ:
		return t.Format("2006-01-02 15:04:05-07")
	}
	return t.Format(dateLayout)
}
