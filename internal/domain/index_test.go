package domain

import "testing"

func TestShouldIndex_Quick(t *testing.T) {
	o := IndexOptions{Depth: IndexDepthQuick}
	cases := []struct {
		role Role
		kind BlockKind
		want bool
	}{
		{RoleUser, KindText, true},
		{RoleAssistant, KindText, false},
		{RoleUser, KindToolResult, true},
		{RoleAssistant, KindToolResult, true},
		{RoleAssistant, KindToolUse, false},
		{RoleAssistant, KindThinking, false},
		{RoleUser, KindImage, false},
	}
	for _, tc := range cases {
		if got := o.ShouldIndex(tc.role, tc.kind); got != tc.want {
			t.Fatalf("ShouldIndex(%s,%s)=%v want %v", tc.role, tc.kind, got, tc.want)
		}
	}
}

func TestShouldIndex_Deep(t *testing.T) {
	o := IndexOptions{Depth: IndexDepthDeep}
	for _, kind := range []BlockKind{KindText, KindThinking, KindToolUse, KindToolResult, KindImage} {
		for _, role := range []Role{RoleUser, RoleAssistant} {
			if !o.ShouldIndex(role, kind) {
				t.Fatalf("deep should index %s/%s", role, kind)
			}
		}
	}
}
