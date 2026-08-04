package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

const (
	agentToken         = "rw1_AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
	providerCredential = "task13-provider-credential-marker"
	providerStderr     = "task13-provider-stderr-marker"
	environmentMarker  = "task13-provider-environment-marker"
	argvMarker         = "task13-api-argv-marker.invalid"
	issueBodyMarker    = "task13-issue-body-marker"
	commentMarker      = "task13-comment-marker"
)

func TestRestrictedGHUsesLoopbackTLSBearerAuthAndTypedProviderCommands(t *testing.T) {
	fixture := newFixture(t)

	issueOutput, issueError := fixture.runGH(t, "issue", "list", "--repo", "alpha/repo")
	if issueError != "" {
		t.Fatalf("issue list stderr = %q", issueError)
	}
	if issueOutput != "31\ttyped issue\topen\tsecurity\t2026-08-01T00:00:00Z\n" {
		t.Fatalf("issue list output = %q", issueOutput)
	}

	checksOutput, checksError := fixture.runGH(t, "pr", "checks", "7", "--repo", "alpha/repo")
	if checksError != "" {
		t.Fatalf("pr checks stderr = %q", checksError)
	}
	wantChecks := "unit\tcompleted\tsuccess\thttps://safe.invalid/check\npolicy\tsuccess\t\thttps://safe.invalid/status\n"
	if checksOutput != wantChecks {
		t.Fatalf("pr checks output = %q, want %q", checksOutput, wantChecks)
	}

	createOutput, createError := fixture.runGH(t, "issue", "create", "--repo", "alpha/repo", "--title", "typed write", "--body", issueBodyMarker)
	if createError != "" || !strings.Contains(createOutput, issueBodyMarker) {
		t.Fatalf("issue create output/stderr = %q / %q", createOutput, createError)
	}
	commentOutput, commentError := fixture.runGH(t, "issue", "comment", "31", "--repo", "alpha/repo", "--body", commentMarker)
	if commentError != "" || commentOutput != "https://safe.invalid/comment/99\n" {
		t.Fatalf("issue comment output/stderr = %q / %q", commentOutput, commentError)
	}

	fixture.stop(t)
	assertProviderContract(t, fixture.providerArgv(), fixture.providerInput())
	assertSafeAudit(t, fixture.audit())
	assertNoFixtureProcess(t, fixture.root)
	assertRepositoryUnchanged(t, fixture.sourceStatus)
}

type fixture struct {
	root               string
	sourceStatus       string
	binaries           testutil.Binaries
	server             *testutil.Server
	providerArgvPath   string
	providerInputPath  string
	providerOutputPath string
	providerEnvPath    string
	providerStderrPath string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	sourceStatus := repositoryStatus(t)
	binDir := filepath.Join(root, "bin")
	binaries := testutil.BuildBinaries(t, binDir)
	provider := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-provider.sh"), filepath.Join(binDir, "fake-provider"))
	ssh := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-ssh.sh"), filepath.Join(binDir, "fake-ssh"))
	certificate := testutil.GenerateCertificate(t, filepath.Join(root, "tls"))
	providerArgvPath := filepath.Join(root, "provider.argv")
	providerInputPath := filepath.Join(root, "provider.stdin")
	providerOutputPath := filepath.Join(root, "provider.stdout")
	providerEnvPath := filepath.Join(root, "provider.env")
	providerStderrPath := filepath.Join(root, "provider.stderr")
	server := testutil.StartServer(t, testutil.ServerOptions{
		Binary: binaries.Service, PolicyPath: filepath.Join("testdata", "policy.yaml"), Certificate: certificate,
		GHPath: provider, SSHPath: ssh,
		Environment: []string{
			"REPOWOLF_TOKEN_AGENT=" + agentToken,
			"GH_TOKEN=" + providerCredential,
			"TASK13_ENV_MARKER=" + environmentMarker,
			"FAKE_PROVIDER_ARGV_LOG=" + providerArgvPath,
			"FAKE_PROVIDER_STDIN_LOG=" + providerInputPath,
			"FAKE_PROVIDER_OUTPUT_LOG=" + providerOutputPath,
			"FAKE_PROVIDER_ENV_LOG=" + providerEnvPath,
			"FAKE_PROVIDER_STDERR_LOG=" + providerStderrPath,
			"FAKE_PROVIDER_STDERR=" + providerStderr,
			"FAKE_PROVIDER_ISSUE_BODY=" + issueBodyMarker,
			"FAKE_PROVIDER_COMMENT=" + commentMarker,
		},
	})
	return &fixture{
		root: root, sourceStatus: sourceStatus, binaries: binaries, server: server,
		providerArgvPath: providerArgvPath, providerInputPath: providerInputPath, providerOutputPath: providerOutputPath,
		providerEnvPath: providerEnvPath, providerStderrPath: providerStderrPath,
	}
}

func (fixture *fixture) clientEnv() []string {
	return testutil.Environment(os.Environ(),
		"HOME="+filepath.Join(fixture.root, "empty-home"),
		"GH_CONFIG_DIR="+filepath.Join(fixture.root, "empty-gh-config"),
		"GIT_CONFIG_NOSYSTEM=1",
		"HTTPS_PROXY=http://127.0.0.1:1", "HTTP_PROXY=http://127.0.0.1:1", "NO_PROXY=127.0.0.1,localhost",
		"REPOWOLF_ENDPOINT="+fixture.server.Endpoint,
		"REPOWOLF_CA_FILE="+fixture.server.Certificate.CAFile,
		"REPOWOLF_SERVER_NAME="+fixture.server.Certificate.ServerName,
		"REPOWOLF_TOKEN="+agentToken,
	)
}

func (fixture *fixture) runGH(t *testing.T, args ...string) (string, string) {
	t.Helper()
	command := exec.Command(fixture.binaries.GH, args...)
	command.Env = fixture.clientEnv()
	command.Dir = fixture.root
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("gh %v: %v; stdout=%q stderr=%q", args, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func (fixture *fixture) stop(t *testing.T)     { t.Helper(); fixture.server.Stop(t) }
func (fixture *fixture) audit() string         { return string(mustRead(fixture.server.AuditPath)) }
func (fixture *fixture) providerArgv() string  { return string(mustRead(fixture.providerArgvPath)) }
func (fixture *fixture) providerInput() string { return string(mustRead(fixture.providerInputPath)) }

func repositoryStatus(t *testing.T) string {
	t.Helper()
	command := exec.Command("git", "status", "--porcelain=v1", "--untracked-files=all")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("read source status: %v", err)
	}
	return string(output)
}

func assertRepositoryUnchanged(t *testing.T, before string) {
	t.Helper()
	if after := repositoryStatus(t); after != before {
		t.Fatalf("integration fixture mutated source tree:\nbefore: %q\nafter:  %q", before, after)
	}
}

func mustRead(path string) []byte {
	contents, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return contents
}

func assertProviderContract(t *testing.T, argv, input string) {
	t.Helper()
	api := func(method, endpoint string, writes bool) []string {
		args := []string{"api", "--hostname", argvMarker, "--method", method,
			"-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"}
		if writes {
			args = append(args, "--input", "-")
		}
		return append(args, endpoint)
	}
	sha := "0123456789012345678901234567890123456789"
	want := [][]string{
		api("GET", "/search/issues?q=repo%3Aalpha%2Frepo+is%3Aissue+state%3Aopen&per_page=30", false),
		api("GET", "/repos/alpha/repo/pulls/7", false),
		api("GET", "/repos/alpha/repo/commits/"+sha+"/check-runs?page=1&per_page=100", false),
		api("GET", "/repos/alpha/repo/commits/"+sha+"/status?page=1&per_page=100", false),
		api("POST", "/repos/alpha/repo/issues", true),
		api("GET", "/repos/alpha/repo/issues/31", false),
		api("POST", "/repos/alpha/repo/issues/31/comments", true),
	}
	if got := recordedBlocks(argv); !reflect.DeepEqual(got, want) {
		t.Errorf("provider argv = %#v\nwant %#v", got, want)
	}
	wantInput := [][]string{{`{"assignees":null,"body":"` + issueBodyMarker + `","labels":null,"title":"typed write"}`}, {`{"body":"` + commentMarker + `"}`}}
	if got := recordedBlocks(input); !reflect.DeepEqual(got, wantInput) {
		t.Errorf("provider input = %#v, want %#v", got, wantInput)
	}
}

func recordedBlocks(contents string) [][]string {
	var blocks [][]string
	var block []string
	for _, line := range strings.Split(strings.TrimSuffix(contents, "\n"), "\n") {
		switch line {
		case "BEGIN":
			block = nil
		case "END":
			blocks = append(blocks, block)
		default:
			block = append(block, line)
		}
	}
	return blocks
}

func assertSafeAudit(t *testing.T, contents string) {
	t.Helper()
	assertAuditInvocations(t, contents, forgeAuditExpectations(
		"github.issue_list", "github.pull_checks", "github.issue_create", "github.issue_comment",
	), auditLeakMarkers())
}
