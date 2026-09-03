package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

func TestIntervalFromString(t *testing.T) {
	t.Run("null yields nil pointer", func(t *testing.T) {
		iv, err := intervalFromString(types.StringNull())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if iv != nil {
			t.Fatalf("got %+v, want nil", iv)
		}
	})

	t.Run("unknown yields nil pointer", func(t *testing.T) {
		iv, err := intervalFromString(types.StringUnknown())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if iv != nil {
			t.Fatalf("got %+v, want nil", iv)
		}
	})

	t.Run("valid ISO 8601", func(t *testing.T) {
		iv, err := intervalFromString(types.StringValue("P1W"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if iv == nil || *iv != (client.Interval{Days: 7}) {
			t.Fatalf("got %+v, want &{Days:7}", iv)
		}
	})

	t.Run("invalid string is an error", func(t *testing.T) {
		if _, err := intervalFromString(types.StringValue("7 days")); err == nil {
			t.Fatal("expected an error for a non-ISO-8601 value")
		}
	})
}

func TestIntervalToString(t *testing.T) {
	sevenDays := &client.Interval{Days: 7}

	t.Run("nil interval yields null", func(t *testing.T) {
		if got := intervalToString(nil, types.StringValue("P1W")); !got.IsNull() {
			t.Fatalf("got %v, want null", got)
		}
	})

	t.Run("canonical form when there is no prior", func(t *testing.T) {
		if got := intervalToString(sevenDays, types.StringNull()); got.ValueString() != "P1W" {
			t.Fatalf("got %q, want %q", got.ValueString(), "P1W")
		}
	})

	t.Run("keeps an equivalent prior verbatim", func(t *testing.T) {
		// "P7D" and "P1W" are the same interval; the API round-trips it as an
		// object and the canonical render is "P1W", but a user who wrote "P7D"
		// must not see a perpetual diff.
		if got := intervalToString(sevenDays, types.StringValue("P7D")); got.ValueString() != "P7D" {
			t.Fatalf("got %q, want the prior %q preserved", got.ValueString(), "P7D")
		}
	})

	t.Run("keeps an exactly-matching prior verbatim", func(t *testing.T) {
		if got := intervalToString(sevenDays, types.StringValue("P1W")); got.ValueString() != "P1W" {
			t.Fatalf("got %q, want %q", got.ValueString(), "P1W")
		}
	})

	t.Run("falls back to canonical when prior is a different interval", func(t *testing.T) {
		if got := intervalToString(&client.Interval{Days: 14}, types.StringValue("P1W")); got.ValueString() != "P2W" {
			t.Fatalf("got %q, want %q", got.ValueString(), "P2W")
		}
	})

	t.Run("ignores an unparseable prior", func(t *testing.T) {
		if got := intervalToString(sevenDays, types.StringValue("garbage")); got.ValueString() != "P1W" {
			t.Fatalf("got %q, want %q", got.ValueString(), "P1W")
		}
	})
}

// TestLayerRespToModelRotationLength is the end-to-end version of the
// diff-suppression path: a layer Read/Update maps the API response back through
// layerRespToModel, which must keep the operator's literal rotation_length when
// it is equivalent to whatever the API returned.
func TestLayerRespToModelRotationLength(t *testing.T) {
	resp := &client.AdminLayerResp{
		RotationLength: &client.Interval{Days: 7},
	}

	got := layerRespToModel(resp, timeouts.Value{}, types.StringValue("P7D"))
	if got.RotationLength.ValueString() != "P7D" {
		t.Fatalf("rotation_length = %q, want the prior %q preserved", got.RotationLength.ValueString(), "P7D")
	}

	got = layerRespToModel(resp, timeouts.Value{}, types.StringNull())
	if got.RotationLength.ValueString() != "P1W" {
		t.Fatalf("rotation_length = %q, want canonical %q", got.RotationLength.ValueString(), "P1W")
	}
}
