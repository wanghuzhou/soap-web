package model

import (
	"strings"
	"testing"
)

func TestValidateSavedResponse(t *testing.T) {
	if err := ValidateSavedResponse(nil); err != nil {
		t.Errorf("nil response rejected: %v", err)
	}
	if err := ValidateSavedResponse(&SavedResponse{Body: "ok"}); err != nil {
		t.Errorf("small response rejected: %v", err)
	}
	big := &SavedResponse{Body: strings.Repeat("x", MaxSavedResponseBytes+1)}
	if err := ValidateSavedResponse(big); err == nil {
		t.Error("oversized response should be rejected")
	}
}

func TestValidateProjectResponses(t *testing.T) {
	project := Project{
		Services: []Service{{
			Operations: []Operation{{
				Calls: []Call{{Title: "ok", Response: &SavedResponse{Body: "ok"}}},
			}},
		}},
	}
	if err := ValidateProjectResponses(project); err != nil {
		t.Errorf("valid project rejected: %v", err)
	}

	project.Services[0].Operations[0].Calls[0].Response.Body =
		strings.Repeat("x", MaxSavedResponseBytes+1)
	if err := ValidateProjectResponses(project); err == nil {
		t.Error("oversized project response should be rejected")
	}
}
