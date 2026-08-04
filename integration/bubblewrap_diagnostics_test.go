//go:build integration && linux

package integration_test

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestBubblewrapFailureDiagnosticRedactsSensitiveChannelContents(t *testing.T) {
	markers := []string{
		"rw1_test-bearer-token",
		"provider-authentication-marker",
		"repowolf-control-marker",
	}
	diagnostic := bubblewrapFailureDiagnostic(errors.New("exit status 1"), map[string][]byte{
		"jail stdout":     []byte(markers[0]),
		"jail stderr":     []byte(markers[1]),
		"server stderr":   []byte(markers[2]),
		"audit":           []byte(strings.Join(markers, "\n")),
		"provider stderr": []byte(strings.Join(markers, "\n")),
	})
	for _, marker := range markers {
		if strings.Contains(diagnostic, marker) {
			t.Fatalf("diagnostic exposed sensitive marker")
		}
	}
	for _, category := range []string{"jail stdout", "jail stderr", "server stderr", "audit", "provider stderr"} {
		if !strings.Contains(diagnostic, category) {
			t.Fatalf("diagnostic omitted channel category %q", category)
		}
	}
}

func TestBubblewrapFailureDiagnosticClassifiesInfrastructureErrors(t *testing.T) {
	for _, test := range []struct {
		name, stderr, category string
	}{
		{name: "mount source", stderr: "bwrap: Unable to mount source on destination\n", category: "mount-source"},
		{name: "missing source", stderr: "bwrap: Can't find source path /redacted\n", category: "missing-path"},
		{name: "namespace", stderr: "bwrap: Creating new namespace failed\n", category: "namespace"},
		{name: "tmp overlay", stderr: "bwrap: Creating --tmp-overlay workdir failed\n", category: "tmp-overlay"},
		{name: "label", stderr: "bwrap: labeling not supported on this system\n", category: "labeling"},
		{name: "pid socket", stderr: "bwrap: Can't create intermediate pids socket\n", category: "pid-socket"},
		{name: "unrecognized", stderr: "bwrap: unexpected infrastructure failure\n", category: "unrecognized"},
		{name: "not bubblewrap", stderr: "non-bubblewrap failure\n", category: "not-bubblewrap"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostic := bubblewrapFailureDiagnostic(errors.New("exit status 1"), map[string][]byte{
				"jail stderr": []byte(test.stderr),
			})
			if !strings.Contains(diagnostic, "jail infrastructure="+test.category) {
				t.Fatalf("diagnostic did not report infrastructure category %q", test.category)
			}
			if strings.Contains(diagnostic, test.stderr) {
				t.Fatal("diagnostic exposed raw jail stderr")
			}
		})
	}
}

func bubblewrapFailureDiagnostic(err error, channels map[string][]byte) string {
	categories := make([]string, 0, len(channels))
	for category, contents := range channels {
		categories = append(categories, fmt.Sprintf("%s=%d bytes", category, len(contents)))
	}
	sort.Strings(categories)
	return fmt.Sprintf("Bubblewrap jail failed: %v; channel byte counts: %s; jail infrastructure=%s", err,
		strings.Join(categories, ", "), bubblewrapInfrastructureCategory(channels["jail stderr"]))
}

func bubblewrapInfrastructureCategory(stderr []byte) string {
	if !bytes.HasPrefix(stderr, []byte("bwrap: ")) {
		return "not-bubblewrap"
	}
	switch {
	case bytes.Contains(stderr, []byte("Unable to mount source on destination")):
		return "mount-source"
	case bytes.Contains(stderr, []byte("Can't find source path")):
		return "missing-path"
	case bytes.Contains(stderr, []byte("Creating new namespace failed")),
		bytes.Contains(stderr, []byte("No permissions to create a new namespace")),
		bytes.Contains(stderr, []byte("Joining specified user namespace failed")):
		return "namespace"
	case bytes.Contains(stderr, []byte("Creating --tmp-overlay")):
		return "tmp-overlay"
	case bytes.Contains(stderr, []byte("labeling not supported on this system")):
		return "labeling"
	case bytes.Contains(stderr, []byte("pids socket")), bytes.Contains(stderr, []byte("pid socket")):
		return "pid-socket"
	default:
		return "unrecognized"
	}
}
