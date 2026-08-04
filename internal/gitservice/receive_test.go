package gitservice

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestReceivePackAcceptsOmittedPortBeforeForwardingExactBytes(t *testing.T) {
	service, capture, _ := receiveExecutableService(t)
	prefix := receivePrefix("refs/heads/feature")
	pack := []byte("PACK\x00payload")
	client := append(append([]byte(nil), prefix...), pack...)
	stream := receiveStreamAtPort(client, 0)

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
	events := decodeAuditEvents(t, auditOutput)
	if len(events) != 2 || events[0].Outcome != audit.OutcomeAccepted || events[1].Outcome != audit.OutcomeDenied {
		t.Fatalf("audit events = %#v", events)
	}
	terminal := events[1]
	if terminal.Repository != "project" || terminal.InputBytes != 0 || terminal.UpdateCount != 1 || len(terminal.Refs) != 1 || terminal.Refs[0] != "refs/heads/main" {
		t.Fatalf("terminal audit = %#v", terminal)
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
	return receiveStreamAtPort(data, 2222)
}

func receiveStreamAtPort(data []byte, port uint32) *memoryStream {
	frames := []*repowolfv1.GitFrame{openFrame("git.example", "trusted-owner", "trusted-repo", port)}
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

func decodeAuditEvents(t *testing.T, output *bytes.Buffer) []audit.Event {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var events []audit.Event
	for decoder.More() {
		var event audit.Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		events = append(events, event)
	}
	return events
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
