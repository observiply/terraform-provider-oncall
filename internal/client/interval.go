package client

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Interval mirrors oncall's pginterval.Interval. The oncall API documents
// rotation_length as an ISO 8601 duration string and accepts one on input, but
// serializes *responses* as an object ({"months":M,"days":D,"micros":U}) so
// existing web-UI clients keep their representation (see the oncall repo's
// internal/pginterval/interval.go). oapi-codegen types the field as *string
// from the swaggertype:"string" annotation, which then fails to unmarshal the
// response object ("cannot unmarshal object into Go struct field ... of type
// string"). A post-generation patch in generate.go rewrites the field to
// *Interval; this type round-trips both wire forms:
//
//   - MarshalJSON emits the canonical ISO 8601 string (for request bodies).
//   - UnmarshalJSON accepts the object form, an ISO 8601 string, or null.
type Interval struct {
	Months int32
	Days   int32
	Micros int64
}

type intervalObject struct {
	Months int32 `json:"months"`
	Days   int32 `json:"days"`
	Micros int64 `json:"micros"`
}

// isoDurationRe matches an ISO 8601 duration. Mirrors the oncall server's
// regexp so the two agree on what parses.
var isoDurationRe = regexp.MustCompile(`^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

func (iv Interval) MarshalJSON() ([]byte, error) {
	return json.Marshal(iv.ISO8601())
}

func (iv *Interval) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" || trimmed == "" {
		*iv = Interval{}
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		parsed, err := ParseISO8601(s)
		if err != nil {
			return err
		}
		*iv = parsed
		return nil
	}
	var obj intervalObject
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("client: rotation_length must be an ISO 8601 string or {months,days,micros} object: %w", err)
	}
	*iv = Interval(obj)
	return nil
}

// ISO8601 renders the interval as a canonical ISO 8601 duration: whole weeks
// are folded out of the day count and whole years out of the month count, so a
// 7-day interval renders "P1W" (matching how the value was most likely
// written). Returns "PT0S" for the zero interval.
func (iv Interval) ISO8601() string {
	years, months := iv.Months/12, iv.Months%12
	weeks, days := iv.Days/7, iv.Days%7

	micros := iv.Micros
	const perHour = int64(time.Hour / time.Microsecond)
	const perMinute = int64(time.Minute / time.Microsecond)
	const perSecond = int64(time.Second / time.Microsecond)
	hours := micros / perHour
	micros -= hours * perHour
	minutes := micros / perMinute
	micros -= minutes * perMinute
	seconds := micros / perSecond
	fracMicros := micros - seconds*perSecond

	var b strings.Builder
	b.WriteByte('P')
	writeUnit(&b, int64(years), "Y")
	writeUnit(&b, int64(months), "M")
	writeUnit(&b, int64(weeks), "W")
	writeUnit(&b, int64(days), "D")
	if hours != 0 || minutes != 0 || seconds != 0 || fracMicros != 0 {
		b.WriteByte('T')
		writeUnit(&b, hours, "H")
		writeUnit(&b, minutes, "M")
		if fracMicros != 0 {
			frac := strings.TrimRight(fmt.Sprintf("%06d", fracMicros), "0")
			fmt.Fprintf(&b, "%d.%sS", seconds, frac)
		} else {
			writeUnit(&b, seconds, "S")
		}
	}
	if b.Len() == 1 {
		return "PT0S"
	}
	return b.String()
}

func writeUnit(b *strings.Builder, n int64, suffix string) {
	if n != 0 {
		fmt.Fprintf(b, "%d%s", n, suffix)
	}
}

// ParseISO8601 parses an ISO 8601 duration into an Interval, folding weeks into
// days (W*7) and years into months (Y*12) exactly as the oncall server does.
func ParseISO8601(value string) (Interval, error) {
	m := isoDurationRe.FindStringSubmatch(value)
	if m == nil || value == "P" || strings.HasSuffix(value, "T") {
		return Interval{}, fmt.Errorf("client: invalid ISO 8601 duration %q", value)
	}

	atoi := func(s string) (int64, error) {
		if s == "" {
			return 0, nil
		}
		return strconv.ParseInt(s, 10, 64)
	}
	years, err := atoi(m[1])
	if err != nil {
		return Interval{}, err
	}
	months, err := atoi(m[2])
	if err != nil {
		return Interval{}, err
	}
	weeks, err := atoi(m[3])
	if err != nil {
		return Interval{}, err
	}
	days, err := atoi(m[4])
	if err != nil {
		return Interval{}, err
	}
	hours, err := atoi(m[5])
	if err != nil {
		return Interval{}, err
	}
	minutes, err := atoi(m[6])
	if err != nil {
		return Interval{}, err
	}

	var seconds time.Duration
	if m[7] != "" {
		d, err := time.ParseDuration(m[7] + "s")
		if err != nil {
			return Interval{}, fmt.Errorf("client: invalid ISO 8601 duration %q: %w", value, err)
		}
		seconds = d
	}

	totalMonths := years*12 + months
	totalDays := weeks*7 + days
	if totalMonths > math.MaxInt32 || totalDays > math.MaxInt32 {
		return Interval{}, fmt.Errorf("client: ISO 8601 duration %q is too large", value)
	}
	micros := (hours*60+minutes)*int64(time.Minute/time.Microsecond) + seconds.Microseconds()
	return Interval{
		Months: int32(totalMonths), //nolint:gosec // bounds checked above
		Days:   int32(totalDays),   //nolint:gosec // bounds checked above
		Micros: micros,
	}, nil
}
