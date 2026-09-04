package domain

import "testing"

func TestDeriveTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "skips user_query tag line",
			in:   "<user_query>\nCan you show the date and hour of each message",
			want: "Can you show the date and hour of each message",
		},
		{
			name: "skips slash command only",
			in:   "/caveman ultra",
			want: "",
		},
		{
			name: "skips manually attached skills tag",
			in:   "<manually_attached_skills>\nThe user has manually attached things",
			want: "The user has manually attached things",
		},
		{
			name: "skips image metadata lines",
			in:   "[Image]\n[Image]\n<image_files>\nThe following images were provided",
			want: "The following images were provided",
		},
		{
			name: "keeps at-prefixed file reference",
			in:   "@ops/terraform/waf/common/allowlist.tf:510 what is this",
			want: "@ops/terraform/waf/common/allowlist.tf:510 what is this",
		},
		{
			name: "strips inline opening tag",
			in:   "<user_query>Can you plan to implement manual indexation",
			want: "Can you plan to implement manual indexation",
		},
		{
			name: "truncates long title",
			in:   stringsRepeat("abcdefghij", 8),
			want: stringsRepeat("abcdefghij", 6) + "…",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveTitle(tc.in, 60)
			if got != tc.want {
				t.Fatalf("DeriveTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func stringsRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
