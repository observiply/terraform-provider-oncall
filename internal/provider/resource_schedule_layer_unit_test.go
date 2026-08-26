package provider

import (
	"testing"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

func TestIntervalToISO8601(t *testing.T) {
	t.Parallel()

	intp := func(v int) *int { return &v }
	tests := []struct {
		name string
		in   *client.PgintervalInterval
		want string
	}{
		{name: "weeks", in: &client.PgintervalInterval{Days: intp(14)}, want: "P2W"},
		{name: "months and days", in: &client.PgintervalInterval{Months: intp(14), Days: intp(3)}, want: "P14M3D"},
		{name: "time", in: &client.PgintervalInterval{Micros: intp(14_706_500_000)}, want: "PT4H5M6.5S"},
		{name: "zero", in: &client.PgintervalInterval{}, want: "PT0S"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := intervalToISO8601(tt.in)
			if got.ValueString() != tt.want {
				t.Fatalf("intervalToISO8601() = %q, want %q", got.ValueString(), tt.want)
			}
		})
	}
}
