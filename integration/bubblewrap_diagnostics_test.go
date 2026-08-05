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
		{name: "uid map", stderr: "bwrap: setting up uid map\n", category: "uid-map"},
		{name: "gid map", stderr: "bwrap: setting up gid map\n", category: "gid-map"},
		{name: "fsuid", stderr: "bwrap: Unable to set fsuid\n", category: "fsuid"},
		{name: "dumpable", stderr: "bwrap: can't set dumpable\n", category: "dumpable"},
		{name: "eventfd", stderr: "bwrap: read eventfd\n", category: "eventfd"},
		{name: "pid namespace", stderr: "bwrap: Setting pidns failed\n", category: "pidns"},
		{name: "pid namespace unshare", stderr: "bwrap: unshare pid ns\n", category: "pidns"},
		{name: "pivot root", stderr: "bwrap: pivot_root\n", category: "pivot-root"},
		{name: "root chdir", stderr: "bwrap: chdir / (base path)\n", category: "root-chdir"},
		{name: "root unmount", stderr: "bwrap: umount old root\n", category: "root-unmount"},
		{name: "root open", stderr: "bwrap: can't open /\n", category: "root-open"},
		{name: "fork pid 1", stderr: "bwrap: Can't fork for pid 1\n", category: "fork-pid1"},
		{name: "setgroups", stderr: "bwrap: error writing to setgroups\n", category: "setgroups"},
		{name: "proc open", stderr: "bwrap: Can't open /proc\n", category: "proc-open"},
		{name: "root propagation", stderr: "bwrap: Failed to make / slave\n", category: "root-propagation"},
		{name: "root tmpfs", stderr: "bwrap: Failed to mount tmpfs\n", category: "root-tmpfs"},
		{name: "root base chdir", stderr: "bwrap: chdir base_path\n", category: "root-chdir"},
		{name: "root directory", stderr: "bwrap: Creating newroot failed\n", category: "root-directory"},
		{name: "root bind", stderr: "bwrap: setting up newroot bind\n", category: "root-bind"},
		{name: "root fchdir", stderr: "bwrap: fchdir to oldroot\n", category: "root-chdir"},
		{name: "privsep socket", stderr: "bwrap: Can't create privsep socket\n", category: "privsep-socket"},
		{name: "fork helper", stderr: "bwrap: Can't fork unprivileged helper\n", category: "fork-helper"},
		{name: "user namespace unshare", stderr: "bwrap: unshare user ns\n", category: "namespace"},
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
		bytes.Contains(stderr, []byte("Joining specified user namespace failed")),
		bytes.Contains(stderr, []byte("unshare user ns")):
		return "namespace"
	case bytes.Contains(stderr, []byte("Creating --tmp-overlay")):
		return "tmp-overlay"
	case bytes.Contains(stderr, []byte("labeling not supported on this system")):
		return "labeling"
	case bytes.Contains(stderr, []byte("pids socket")), bytes.Contains(stderr, []byte("pid socket")):
		return "pid-socket"
	case bytes.Contains(stderr, []byte("setting up uid map")):
		return "uid-map"
	case bytes.Contains(stderr, []byte("setting up gid map")):
		return "gid-map"
	case bytes.Contains(stderr, []byte("fsuid")):
		return "fsuid"
	case bytes.Contains(stderr, []byte("can't set dumpable")):
		return "dumpable"
	case bytes.Contains(stderr, []byte("eventfd")):
		return "eventfd"
	case bytes.Contains(stderr, []byte("Setting pidns failed")), bytes.Contains(stderr, []byte("unshare pid ns")):
		return "pidns"
	case bytes.Contains(stderr, []byte("pivot_root")):
		return "pivot-root"
	case bytes.Contains(stderr, []byte("chdir /")):
		return "root-chdir"
	case bytes.Contains(stderr, []byte("unmount old root")), bytes.Contains(stderr, []byte("umount old root")):
		return "root-unmount"
	case bytes.Contains(stderr, []byte("can't open /")):
		return "root-open"
	case bytes.Contains(stderr, []byte("Can't fork for pid 1")):
		return "fork-pid1"
	case bytes.Contains(stderr, []byte("error writing to setgroups")):
		return "setgroups"
	case bytes.Contains(stderr, []byte("Can't open /proc")):
		return "proc-open"
	case bytes.Contains(stderr, []byte("Failed to make / slave")):
		return "root-propagation"
	case bytes.Contains(stderr, []byte("Failed to mount tmpfs")):
		return "root-tmpfs"
	case bytes.Contains(stderr, []byte("chdir base_path")), bytes.Contains(stderr, []byte("fchdir to oldroot")):
		return "root-chdir"
	case bytes.Contains(stderr, []byte("Creating newroot failed")), bytes.Contains(stderr, []byte("Creating oldroot failed")):
		return "root-directory"
	case bytes.Contains(stderr, []byte("setting up newroot bind")):
		return "root-bind"
	case bytes.Contains(stderr, []byte("Can't create privsep socket")):
		return "privsep-socket"
	case bytes.Contains(stderr, []byte("Can't fork unprivileged helper")):
		return "fork-helper"
	default:
		return "unrecognized"
	}
}
