package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

const (
	pinnedGiteaProviderMarker  = "pinned-gitea-provider-token-marker"
	pinnedGiteaPrincipalMarker = "rw1_AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
)

func TestPinnedGiteaGitInteroperability(t *testing.T) {
	if os.Getenv("REPOWOLF_GITEA_INTEGRATION") != "1" {
		t.Skip("set REPOWOLF_GITEA_INTEGRATION=1 to run pinned Gitea interoperability")
	}
	fixture := newPinnedGiteaGitFixture(t)
	clone, fetch := fixture.cloneAndFetch(t)
	allowed, denied := fixture.pushAllowedAndDenied(t)
	cancelledOutput := fixture.cancelActivePush(t)
	fixture.server.Stop(t)
	fixture.assertCompletion(t, clone, fetch, allowed, denied, cancelledOutput)
}

func (fixture *pinnedGiteaGitFixture) assertCompletion(t *testing.T, clone, fetch, allowed, denied pinnedGitResult, cancelledOutput string) {
	t.Helper()
	auditLog := string(mustRead(fixture.server.AuditPath))
	captureLog := string(mustRead(fixture.capture))
	checkoutContents := string(mustRead(filepath.Join(fixture.checkout, "seed.txt")))
	assertPinnedGiteaLeaks(t, fixture, clone, fetch, allowed, denied, cancelledOutput, auditLog, captureLog, checkoutContents)
	assertPinnedGiteaAudit(t, auditLog)
	assertPinnedGiteaSSHInvocations(t, fixture.gitea, captureLog)
}

func assertPinnedGiteaLeaks(t *testing.T, fixture *pinnedGiteaGitFixture, clone, fetch, allowed, denied pinnedGitResult, cancelledOutput, auditLog, captureLog, checkoutContents string) {
	t.Helper()
	for channel, contents := range map[string]string{
		"clone stdout": clone.stdout, "clone stderr": clone.stderr,
		"fetch stdout": fetch.stdout, "fetch stderr": fetch.stderr,
		"allowed push stdout": allowed.stdout, "allowed push stderr": allowed.stderr,
		"denied push stdout": denied.stdout, "denied push stderr": denied.stderr,
		"cancelled push": cancelledOutput, "receive capture": string(mustRead(fixture.receiveCapture)),
		"broker stderr": string(mustRead(fixture.server.StderrPath)), "audit": auditLog,
		"checkout": checkoutContents, "ssh environment": captureLog,
	} {
		for _, marker := range []string{pinnedGiteaProviderMarker, pinnedGiteaPrincipalMarker} {
			if strings.Contains(contents, marker) {
				t.Errorf("%s leaked provider credential marker", channel)
			}
		}
	}
}

func assertPinnedGiteaAudit(t *testing.T, auditLog string) {
	t.Helper()
	if providerEvents, repositoryEvents := strings.Count(auditLog, `"provider":"gitea"`), strings.Count(auditLog, `"repository":"pinned-gitea"`); providerEvents != 10 || repositoryEvents != 10 {
		t.Fatalf("missing Gitea Git audit pairs: providerEvents=%d repositoryEvents=%d auditBytes=%d", providerEvents, repositoryEvents, len(auditLog))
	}
	records, err := parseAuditRecords([]byte(auditLog), []string{pinnedGiteaProviderMarker, pinnedGiteaPrincipalMarker})
	if err != nil {
		t.Fatal(err)
	}
	var lifecycle []string
	var receiveTerminals []auditRecord
	for _, record := range records {
		if record.event.Provider != "gitea" {
			continue
		}
		lifecycle = append(lifecycle, record.event.Operation+":"+string(record.event.Outcome))
		if record.event.Operation == "git.receive-pack" && string(record.event.Outcome) != "accepted" {
			receiveTerminals = append(receiveTerminals, record)
		}
	}
	wantLifecycle := []string{
		"git.upload-pack:accepted", "git.upload-pack:completed",
		"git.upload-pack:accepted", "git.upload-pack:completed",
		"git.receive-pack:accepted", "git.receive-pack:completed",
		"git.receive-pack:accepted", "git.receive-pack:denied",
		"git.receive-pack:accepted", "git.receive-pack:cancelled",
	}
	if !reflect.DeepEqual(lifecycle, wantLifecycle) {
		t.Fatalf("Gitea audit lifecycle = %v, want %v", lifecycle, wantLifecycle)
	}
	assertPinnedGiteaReceiveTerminals(t, receiveTerminals)
}

func assertPinnedGiteaReceiveTerminals(t *testing.T, terminals []auditRecord) {
	t.Helper()
	if len(terminals) != 3 {
		t.Fatalf("receive-pack terminal audit count = %d, want 3", len(terminals))
	}
	allowed, denied, cancelled := terminals[0], terminals[1], terminals[2]
	if string(allowed.event.Outcome) != "completed" || allowed.event.Reason != "GIT_TERMINAL_CATEGORY_COMPLETED" || !reflect.DeepEqual(allowed.event.Refs, []string{"refs/heads/feature/allowed"}) || allowed.event.UpdateCount != 1 || allowed.event.InputBytes <= 0 || allowed.event.OutputBytes <= 0 || !allowed.fields["input_bytes"] || !allowed.fields["output_bytes"] {
		t.Fatalf("unsafe or incomplete allowed receive-pack terminal audit: %#v fields=%v", allowed.event, allowed.fields)
	}
	if string(denied.event.Outcome) != "denied" || denied.event.Reason != "GIT_TERMINAL_CATEGORY_INVALID_REQUEST" || !reflect.DeepEqual(denied.event.Refs, []string{"refs/heads/denied"}) || denied.event.UpdateCount != 1 || denied.event.InputBytes != 0 || denied.fields["input_bytes"] || denied.event.OutputBytes <= 0 || !denied.fields["output_bytes"] {
		t.Fatalf("unsafe or incomplete denied receive-pack terminal audit: %#v fields=%v", denied.event, denied.fields)
	}
	if string(cancelled.event.Outcome) != "cancelled" || cancelled.event.Reason != "GIT_TERMINAL_CATEGORY_UNAVAILABLE" || len(cancelled.event.Refs) != 0 || cancelled.event.UpdateCount != 0 || cancelled.event.InputBytes != 0 || cancelled.fields["input_bytes"] {
		t.Fatalf("unsafe or incomplete cancelled receive-pack terminal audit: %#v fields=%v", cancelled.event, cancelled.fields)
	}
}

func assertPinnedGiteaSSHInvocations(t *testing.T, gitea *testutil.Gitea, captureLog string) {
	t.Helper()
	invocations := strings.Count(captureLog, "BEGIN\n")
	giteaTokenUnset := strings.Contains(captureLog, "REPOWOLF_TOKEN_GITEA=unset")
	principalTokenUnset := strings.Contains(captureLog, "REPOWOLF_TOKEN_AGENT=unset")
	if invocations != 5 || !giteaTokenUnset || !principalTokenUnset {
		t.Fatalf("unexpected SSH environment capture: invocations=%d giteaTokenUnset=%t principalTokenUnset=%t captureBytes=%d", invocations, giteaTokenUnset, principalTokenUnset, len(captureLog))
	}
	trustedPrefix := "-T\n-p\n" + strconv.Itoa(gitea.SSHPort) + "\n--\n" + gitea.SSHUser + "@" + gitea.SSHHost + "\n"
	trustedUploads := strings.Count(captureLog, trustedPrefix+"git-upload-pack '"+gitea.Owner+"/"+gitea.Repository+".git'")
	trustedReceives := strings.Count(captureLog, trustedPrefix+"git-receive-pack '"+gitea.Owner+"/"+gitea.Repository+".git'")
	if trustedUploads != 2 || trustedReceives != 3 {
		t.Fatalf("SSH wrapper did not preserve trusted argv: uploads=%d receives=%d captureBytes=%d", trustedUploads, trustedReceives, len(captureLog))
	}
}

type pinnedGitResult struct {
	stdout, stderr string
	err            error
}

func runPinnedGit(gitPath, directory string, environment []string, args ...string) pinnedGitResult {
	command := exec.Command(gitPath, args...)
	command.Dir, command.Env = directory, environment
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return pinnedGitResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func runPinnedGitOK(t *testing.T, gitPath, directory string, environment []string, args ...string) pinnedGitResult {
	t.Helper()
	result := runPinnedGit(gitPath, directory, environment, args...)
	if result.err != nil {
		t.Fatalf("git %v: %v; stdoutBytes=%d stderrBytes=%d", args, result.err, len(result.stdout), len(result.stderr))
	}
	return result
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}
