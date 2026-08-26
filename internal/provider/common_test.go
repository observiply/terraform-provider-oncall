package provider

import (
	"encoding/json"
	"testing"

	"github.com/observiply/terraform-provider-oncall/internal/client"
)

func TestNewBodyEncodesGeneratedUnion(t *testing.T) {
	name := "acceptance-schedule"
	teamIDs := []string{"team-a", "team-b"}

	body, err := newBody(
		(*client.PostAdminSchedulesJSONBody).FromAdminCreateScheduleBody,
		client.AdminCreateScheduleBody{Name: &name, TeamIds: &teamIDs},
	)
	if err != nil {
		t.Fatalf("newBody: %v", err)
	}

	var decoded client.AdminCreateScheduleBody
	if err := json.NewDecoder(body).Decode(&decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if decoded.Name == nil || *decoded.Name != name {
		t.Fatalf("name = %v, want %q", decoded.Name, name)
	}
	if decoded.TeamIds == nil || len(*decoded.TeamIds) != 2 || (*decoded.TeamIds)[0] != "team-a" || (*decoded.TeamIds)[1] != "team-b" {
		t.Fatalf("team_ids = %v, want %v", decoded.TeamIds, teamIDs)
	}
}
