package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const defaultOperationTimeoutSeconds = 30

// stringsToSet converts a Go string slice to a Terraform types.Set of strings.
func stringsToSet(ctx context.Context, ids []string) (types.Set, diag.Diagnostics) {
	return types.SetValueFrom(ctx, types.StringType, ids)
}

// setToStrings converts a Terraform types.Set of strings to a Go slice. A
// null or unknown set yields an empty slice rather than an error.
func setToStrings(ctx context.Context, s types.Set) ([]string, diag.Diagnostics) {
	if s.IsNull() || s.IsUnknown() {
		return nil, nil
	}
	var out []string
	diags := s.ElementsAs(ctx, &out, false)
	return out, diags
}

// normalizeTeamIDs returns team_ids with the owner team guaranteed present
// (first), deduplicated, and the remainder sorted for a stable order. Per
// AGENTS.md's sharing model, a PUT .../teams payload that drops the owner
// team is rejected by validateShareTeamIDs (400) — this applies that rule
// uniformly across schedules/triggers/integrations, and the stable ordering
// keeps repeat applies from producing spurious diffs.
func normalizeTeamIDs(ownerID string, ids []string) []string {
	seen := map[string]bool{ownerID: true}
	rest := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		rest = append(rest, id)
	}
	sort.Strings(rest)
	return append([]string{ownerID}, rest...)
}

// derefStrSlice dereferences a *[]string from a generated response type,
// returning nil (not a panic) when the API omitted the field.
func derefStrSlice(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

// strFromPtr converts an API response's *string into a Terraform value,
// mapping a nil pointer (field omitted by the API) to null rather than "".
func strFromPtr(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// boolFromPtr converts an API response's *bool into a Terraform value.
func boolFromPtr(p *bool) types.Bool {
	if p == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*p)
}

// int64FromIntPtr converts an API response's *int into a Terraform Int64
// value (the generated client uses *int; the framework only has Int64).
func int64FromIntPtr(p *int) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*p))
}

// intPtrFromInt64 converts a Terraform Int64 value into the *int the
// generated client expects, returning nil for null/unknown.
func intPtrFromInt64(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

// newBody builds one of the generated client's "oneOf" request-body wrapper
// types (client.PostAdminSchedulesJSONBody and friends — an artifact of the
// `oneOf: [{type: object}, $ref]` shape swag emits for every documented
// body) by calling its generated From<Schema> setter. Call as e.g.
// newBody((*client.PostAdminSchedulesJSONBody).FromAdminCreateScheduleBody, client.AdminCreateScheduleBody{...}).
// The setter only errors if the value fails to json.Marshal, which cannot
// happen for these plain pointer-field structs.
func newBody[B any, V any](set func(*B, V) error, v V) (B, error) {
	var b B
	err := set(&b, v)
	return b, err
}

// unexpectedStatus builds a diagnostic for an HTTP response the caller
// didn't otherwise handle explicitly (i.e. anything but the documented
// success/404 cases).
func unexpectedStatus(summary, method, path string, statusCode int, body []byte) diag.Diagnostic {
	return diag.NewErrorDiagnostic(
		summary,
		fmt.Sprintf("%s %s returned unexpected HTTP %d: %s", method, path, statusCode, string(body)),
	)
}

// diagsFromError wraps a plain Go error as a one-element diag.Diagnostics,
// for helpers that return (value, diagnostics) instead of taking a
// *diag.Diagnostics to append to.
func diagsFromError(summary string, err error) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.AddError(summary, err.Error())
	return diags
}

// diagsFromDiagnostic wraps a single diagnostic as a diag.Diagnostics.
func diagsFromDiagnostic(d diag.Diagnostic) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(d)
	return diags
}
