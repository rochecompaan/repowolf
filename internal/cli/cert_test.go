package cli_test

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/cli"
)

func TestRunCertInitParsesRepeatableSANsAndPrintsOnlyPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certificates")
	var stdout bytes.Buffer
	args := []string{"--output", dir, "--dns", "one.internal", "--dns", "two.internal", "--ip", "127.0.0.1", "--ip", "::1"}
	if err := cli.RunCertInit(args, &stdout, fixedNow, certEntropy()); err != nil {
		t.Fatalf("RunCertInit() error = %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("output lines = %q, want four paths", lines)
	}
	for _, path := range lines {
		if filepath.Dir(path) != dir {
			t.Fatalf("output path = %q, want path in %q", path, dir)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("output path %q: %v", path, err)
		}
	}
	if strings.Contains(stdout.String(), "PRIVATE KEY") {
		t.Fatal("output contains private key PEM")
	}

	certificatePEM, err := os.ReadFile(filepath.Join(dir, "tls.crt"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		t.Fatal("server certificate is not PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(certificate.DNSNames, []string{"one.internal", "two.internal"}) {
		t.Fatalf("DNS SANs = %v", certificate.DNSNames)
	}
	if len(certificate.IPAddresses) != 2 {
		t.Fatalf("IP SANs = %v", certificate.IPAddresses)
	}
	gotIPs := []string{certificate.IPAddresses[0].String(), certificate.IPAddresses[1].String()}
	if !reflect.DeepEqual(gotIPs, []string{"127.0.0.1", "::1"}) {
		t.Fatalf("IP SANs = %v", gotIPs)
	}
}

func TestRunCertInitRejectsInvalidArgumentsWithoutEchoingThem(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing output", args: []string{"--dns", "repo.internal"}},
		{name: "missing SAN", args: []string{"--output", t.TempDir()}},
		{name: "invalid IP", args: []string{"--output", t.TempDir(), "--ip", "secret-invalid-ip"}},
		{name: "empty DNS", args: []string{"--output", t.TempDir(), "--dns", ""}},
		{name: "positional", args: []string{"--output", t.TempDir(), "--dns", "repo.internal", "secret-positional"}},
		{name: "unknown flag", args: []string{"--output", t.TempDir(), "--dns", "repo.internal", "--secret-flag"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := cli.RunCertInit(test.args, &stdout, fixedNow, certEntropy())
			if !errors.Is(err, cli.ErrInvalidCertArguments) {
				t.Fatalf("RunCertInit() error = %v, want ErrInvalidCertArguments", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			for _, secret := range []string{"secret-invalid-ip", "secret-positional", "secret-flag"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error %q echoed input %q", err, secret)
				}
			}
		})
	}
}

func TestRunCertInitReturnsFixedWriterError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certificates")
	writer := &payloadErrorWriter{}
	err := cli.RunCertInit([]string{"--output", dir, "--dns", "repo.internal"}, writer, fixedNow, certEntropy())
	if err == nil || err.Error() != "failed to write certificate paths" {
		t.Fatalf("RunCertInit() error = %v", err)
	}
	if strings.Contains(err.Error(), writer.payload) || strings.Contains(err.Error(), "payload-derived") {
		t.Fatal("RunCertInit() disclosed writer-provided text")
	}
}

func fixedNow() time.Time {
	return time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
}

func certEntropy() *bytes.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{0x8d}, 128))
}
