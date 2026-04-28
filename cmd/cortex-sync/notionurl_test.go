package main

import "testing"

func TestExtractDatabaseID(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"raw32", "c556c2b71a2f46fb938787fc0f7a6016", "c556c2b71a2f46fb938787fc0f7a6016", false},
		{"raw32_uppercase", "C556C2B71A2F46FB938787FC0F7A6016", "c556c2b71a2f46fb938787fc0f7a6016", false},
		{"dashed", "c556c2b7-1a2f-46fb-9387-87fc0f7a6016", "c556c2b71a2f46fb938787fc0f7a6016", false},
		{"url_short", "https://www.notion.so/c556c2b71a2f46fb938787fc0f7a6016", "c556c2b71a2f46fb938787fc0f7a6016", false},
		{"url_with_view", "https://www.notion.so/c556c2b71a2f46fb938787fc0f7a6016?v=11111111111111111111111111111111", "c556c2b71a2f46fb938787fc0f7a6016", false},
		{"url_with_workspace_and_title", "https://www.notion.so/myws/My-Memories-c556c2b71a2f46fb938787fc0f7a6016?v=22222222222222222222222222222222", "c556c2b71a2f46fb938787fc0f7a6016", false},
		{"empty", "", "", true},
		{"too_short", "abc", "", true},
		{"no_hex", "https://www.notion.so/My-Page", "", true},
		{"trim_whitespace", "   c556c2b71a2f46fb938787fc0f7a6016  ", "c556c2b71a2f46fb938787fc0f7a6016", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractDatabaseID(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
