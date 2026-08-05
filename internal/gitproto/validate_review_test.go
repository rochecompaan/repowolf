package gitproto

import "testing"

func TestValidRefRejectsGitInvalidPatternsAndASCIIControls(t *testing.T) {
	for _, ref := range []string{
		"refs/heads/a..b",
		"refs/heads/a//b",
		"refs/heads/a@{b",
		"refs/heads/.hidden",
		"refs/heads/name.lock",
		"refs/heads/name.",
		"refs/heads/name^old",
		"refs/heads/name?old",
		"refs/heads/name*old",
		"refs/heads/name[old",
		"refs/heads/name\\old",
		"refs/heads/name\x01",
		"refs/heads/name\x1f",
		"refs/heads/name\x7f",
	} {
		if validRef(ref) {
			t.Errorf("validRef(%q) = true, want false", ref)
		}
	}
}

func TestValidRefAcceptsFullyQualifiedFeatureRef(t *testing.T) {
	for _, ref := range []string{"refs/heads/feature/test-1", "refs/heads/@"} {
		if !validRef(ref) {
			t.Fatalf("validRef(%q) = false, want true", ref)
		}
	}
}
