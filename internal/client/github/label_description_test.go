package github

import (
	"strings"
	"testing"
)

func TestLabelCreateDescriptionPresence(t *testing.T) {
	cwd := newGitHubRepository(t)
	for _, test := range []struct {
		name  string
		args  []string
		valid bool
	}{
		{"empty", []string{"--description", ""}, true},
		{"maximum code points", []string{"--description", strings.Repeat("é", 100)}, true},
		{"absent", nil, false},
		{"missing value", []string{"--description"}, false},
		{"oversized", []string{"--description", strings.Repeat("é", 101)}, false},
		{"invalid UTF-8", []string{"--description", "\xff"}, false},
		{"NUL", []string{"--description", "bad\x00text"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"label", "create", "bug", "--color", "123456"}, test.args...)
			request, err := Parse(args, cwd)
			if !test.valid {
				if err == nil {
					t.Fatal("accepted invalid description")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := request.GetLabelCreate(); got == nil || got.GetDescription() != test.args[1] {
				t.Fatalf("label create = %v", got)
			}
		})
	}
}
