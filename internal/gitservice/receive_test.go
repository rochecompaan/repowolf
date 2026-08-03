package gitservice

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const (
	testOID1 = "1111111111111111111111111111111111111111"
	testOID2 = "2222222222222222222222222222222222222222"
)

func TestReceivePackValidatesPrefixBeforeForwardingExactBytes(t *testing.T) {
	service, capture, _ := receiveExecutableService(t)
	prefix := receivePrefix("refs/heads/feature")
	pack := []byte("PACK\x00payload")
	client := append(append([]byte(nil), prefix...), pack...)
	stream := receiveStream(client)

	if err := service.receivePack(stream); err != nil {
		t.Fatalf("receivePack: %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, client) {
		t.Fatalf("provider input = %q, want exact %q", got, client)
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED)
	if len(stream.sent) < 2 || !bytes.Equal(stream.sent[0].GetData().GetData(), advertisement()) {
		t.Fatalf("advertisement not relayed exactly: %#v", stream.sent)
	}
}

func TestReceivePackDeniedRefForwardsZeroClientBytes(t *testing.T) {
	service, capture, auditOutput := receiveExecutableService(t)
	stream := receiveStream(receivePrefix("refs/heads/main"))

	if err := service.receivePack(stream); err != nil {
		t.Fatalf("receivePack: %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("provider received %d denied update bytes", len(got))
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST)
	if strings.Contains(auditOutput.String(), "input_bytes") {
		t.Fatalf("denied push audit reported provider input: %s", auditOutput.String())
	}
}

func TestReceivePackMalformedPrefixForwardsZeroClientBytes(t *testing.T) {
	service, capture, _ := receiveExecutableService(t)
	stream := receiveStream([]byte("zzzzmalformed"))

	if err := service.receivePack(stream); err != nil {
		t.Fatalf("receivePack: %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("provider received %d malformed update bytes", len(got))
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST)
}

func receiveExecutableService(t *testing.T) (*Service, string, *bytes.Buffer) {
	t.Helper()
	service := newTestService(t, config.GitRead, config.GitWrite)
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh")
	capture := filepath.Join(directory, "provider-input")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + shell + "\n" +
		"[ \"$6\" = \"git-receive-pack 'trusted-owner/trusted-repo.git'\" ] || exit 92\n" +
		"printf '" + shellOctal(advertisement()) + "'\n" +
		"exec \"$CAT\" >\"$CAPTURE\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service.options.SSHPath = path
	service.options.Environment = []string{"CAT=" + cat, "CAPTURE=" + capture}
	service.options.Runner = &runner.Runner{}
	auditOutput := &bytes.Buffer{}
	service.options.Audit = audit.NewWriter(auditOutput)
	return service, capture, auditOutput
}

func receiveStream(data []byte) *memoryStream {
	frames := []*repowolfv1.GitFrame{openFrame("git.example", "trusted-owner", "trusted-repo", 2222)}
	for len(data) > 0 {
		size := len(data)
		if size > 17 {
			size = 17
		}
		frames = append(frames, dataFrame(append([]byte(nil), data[:size]...)))
		data = data[size:]
	}
	return &memoryStream{ctx: auth.WithPrincipal(context.Background(), "agent"), received: frames}
}

func advertisement() []byte {
	return append(pkt(testOID1+" refs/heads/feature\x00report-status delete-refs\n"), []byte("0000")...)
}

func receivePrefix(ref string) []byte {
	return append(pkt(testOID1+" "+testOID2+" "+ref+"\x00report-status"), []byte("0000")...)
}

func pkt(payload string) []byte { return []byte(fmt.Sprintf("%04x%s", len(payload)+4, payload)) }

func shellOctal(data []byte) string {
	var result strings.Builder
	for _, value := range data {
		fmt.Fprintf(&result, "\\%03o", value)
	}
	return result.String()
}

func assertTerminalCategory(t *testing.T, stream *memoryStream, want repowolfv1.GitTerminalCategory) {
	t.Helper()
	if len(stream.sent) == 0 {
		t.Fatal("no server frames")
	}
	terminal := stream.sent[len(stream.sent)-1].GetTerminal()
	if terminal == nil || terminal.Category != want {
		t.Fatalf("terminal = %#v, want %s", terminal, want)
	}
}
