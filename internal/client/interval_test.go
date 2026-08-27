package client

import (
	"encoding/json"
	"testing"
)

func TestIntervalUnmarshalObject(t *testing.T) {
	var iv Interval
	if err := json.Unmarshal([]byte(`{"months":0,"days":7,"micros":0}`), &iv); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if iv != (Interval{Days: 7}) {
		t.Fatalf("got %+v", iv)
	}
	if got := iv.ISO8601(); got != "P1W" {
		t.Fatalf("ISO8601 = %q, want P1W", got)
	}
}

func TestIntervalUnmarshalString(t *testing.T) {
	var iv Interval
	if err := json.Unmarshal([]byte(`"P2W"`), &iv); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if iv != (Interval{Days: 14}) {
		t.Fatalf("got %+v", iv)
	}
}

func TestIntervalMarshalEmitsISOString(t *testing.T) {
	b, err := json.Marshal(Interval{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"P1W"` {
		t.Fatalf("marshal = %s, want \"P1W\"", b)
	}
}

func TestParseISO8601RoundTrip(t *testing.T) {
	cases := map[string]Interval{
		"P1W":     {Days: 7},
		"P2W":     {Days: 14},
		"P1D":     {Days: 1},
		"PT8H":    {Micros: 8 * 3600 * 1_000_000},
		"P1DT12H": {Days: 1, Micros: 12 * 3600 * 1_000_000},
		"P1Y2M":   {Months: 14},
		"PT30M":   {Micros: 30 * 60 * 1_000_000},
		"PT1M30S": {Micros: 90 * 1_000_000},
		"PT0.5S":  {Micros: 500_000},
	}
	for in, want := range cases {
		got, err := ParseISO8601(in)
		if err != nil {
			t.Errorf("ParseISO8601(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseISO8601(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParseISO8601Invalid(t *testing.T) {
	for _, in := range []string{"", "P", "1W", "PT", "P1X", "garbage"} {
		if _, err := ParseISO8601(in); err == nil {
			t.Errorf("ParseISO8601(%q) succeeded, want error", in)
		}
	}
}
