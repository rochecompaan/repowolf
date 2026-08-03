package gitssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestServerFrameStateAcceptsOnlyDataThenOneTerminal(t *testing.T) {
	completed := terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0)
	for name, frames := range map[string][]*repowolfv1.GitFrame{
		"nil":                  {nil},
		"empty":                {{}},
		"open":                 {openFrame(testRequest(UploadPack))},
		"oversized data":       {dataFrame(make([]byte, maximumChunkBytes+1))},
		"unspecified terminal": {terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_UNSPECIFIED, 1)},
		"completed nonzero":    {terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 1)},
		"failure zero":         {terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE, 0)},
		"duplicate terminal":   {completed, completed},
		"data after terminal":  {completed, dataFrame([]byte("late"))},
	} {
		t.Run(name, func(t *testing.T) {
			var state serverFrameState
			for _, frame := range frames {
				if _, err := state.Accept(frame); err != nil {
					return
				}
			}
			t.Fatal("accepted invalid server frame sequence")
		})
	}

	var state serverFrameState
	if terminal, err := state.Accept(dataFrame([]byte("output"))); err != nil || terminal {
		t.Fatalf("data Accept() = %v, %v", terminal, err)
	}
	if terminal, err := state.Accept(completed); err != nil || !terminal {
		t.Fatalf("terminal Accept() = %v, %v", terminal, err)
	}
}

func TestRelayPreservesUploadAndReceiveBytesInBoundedFrames(t *testing.T) {
	for _, operation := range []Operation{UploadPack, ReceivePack} {
		t.Run(string(operation), func(t *testing.T) {
			input := bytes.Repeat([]byte("pack"), 40_000)
			var received bytes.Buffer
			var frameSizes []int
			opener, stop := startRelayServiceExpected(t, operation, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
				open, err := stream.Recv()
				if err != nil || open.GetOpen().GetRepository().GetSshPort() != 2222 {
					return errors.New("missing typed open")
				}
				if err := stream.Send(dataFrame([]byte("advertisement"))); err != nil {
					return err
				}
				for {
					frame, err := stream.Recv()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil || frame.GetData() == nil {
						return errors.New("invalid client data")
					}
					data := frame.GetData().GetData()
					frameSizes = append(frameSizes, len(data))
					received.Write(data)
				}
				output := received.Bytes()
				for len(output) > 0 {
					size := min(len(output), maximumChunkBytes)
					if err := stream.Send(dataFrame(output[:size])); err != nil {
						return err
					}
					output = output[size:]
				}
				return stream.Send(terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0))
			})
			defer stop()

			var stdout bytes.Buffer
			terminal, err := relay(context.Background(), opener, testRequest(operation), bytes.NewReader(input), &stdout)
			if err != nil || terminal.GetExitCode() != 0 {
				t.Fatalf("relay() terminal=%v err=%v", terminal, err)
			}
			if !bytes.Equal(received.Bytes(), input) || !bytes.Equal(stdout.Bytes(), append([]byte("advertisement"), input...)) {
				t.Fatal("relay changed Git bytes")
			}
			if len(frameSizes) != 3 || frameSizes[0] != maximumChunkBytes || frameSizes[1] != maximumChunkBytes || frameSizes[2] != len(input)-2*maximumChunkBytes {
				t.Fatalf("client frame sizes = %v", frameSizes)
			}
		})
	}
}

func TestRelayDoesNotReadStdinWhenServerDeniesOpen(t *testing.T) {
	reader := &countingReader{reader: bytes.NewReader([]byte("must not forward"))}
	opener, stop := startRelayService(t, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}
		return stream.Send(terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PERMISSION_DENIED, 1))
	})
	defer stop()
	terminal, err := relay(context.Background(), opener, testRequest(ReceivePack), reader, io.Discard)
	if err != nil || terminal.GetCategory() != repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PERMISSION_DENIED {
		t.Fatalf("relay() terminal=%v err=%v", terminal, err)
	}
	if reader.reads != 0 {
		t.Fatalf("stdin read %d times before denial", reader.reads)
	}
}

func TestRelayRejectsInvalidServerFramesAndDisconnect(t *testing.T) {
	for name, handler := range map[string]func(grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error{
		"server disconnect": func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
			_, _ = stream.Recv()
			return errors.New("private server detail")
		},
		"server open": func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
			_, _ = stream.Recv()
			return stream.Send(openFrame(testRequest(UploadPack)))
		},
		"oversized data": func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
			_, _ = stream.Recv()
			return stream.Send(dataFrame(make([]byte, maximumChunkBytes+1)))
		},
		"invalid terminal": func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
			_, _ = stream.Recv()
			return stream.Send(terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_UNSPECIFIED, 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			opener, stop := startRelayService(t, handler)
			defer stop()
			if terminal, err := relay(context.Background(), opener, testRequest(UploadPack), bytes.NewReader(nil), io.Discard); err == nil {
				t.Fatalf("relay accepted terminal %v", terminal)
			}
		})
	}
}

func TestRelayCancelsBlockedInputAndDisconnectsOnLocalFailures(t *testing.T) {
	t.Run("context cancellation", func(t *testing.T) {
		serverDone := make(chan struct{})
		opener, stop := startRelayService(t, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
			defer close(serverDone)
			if _, err := stream.Recv(); err != nil {
				return err
			}
			if err := stream.Send(dataFrame([]byte("advertisement"))); err != nil {
				return err
			}
			<-stream.Context().Done()
			return stream.Context().Err()
		})
		defer stop()
		inputReader, inputWriter := io.Pipe()
		defer inputWriter.Close()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := relay(ctx, opener, testRequest(UploadPack), inputReader, io.Discard)
			done <- err
		}()
		time.Sleep(20 * time.Millisecond)
		cancel()
		assertChannelClosed(t, done, "relay cancellation")
		assertChannelClosed(t, serverDone, "server cancellation")
	})

	for name, streams := range map[string]struct {
		input  io.Reader
		output io.Writer
	}{
		"stdin failure":  {input: failingReader{}, output: io.Discard},
		"stdout failure": {input: bytes.NewReader(nil), output: failingWriter{}},
	} {
		t.Run(name, func(t *testing.T) {
			serverDone := make(chan struct{})
			opener, stop := startRelayService(t, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
				defer close(serverDone)
				if _, err := stream.Recv(); err != nil {
					return err
				}
				if err := stream.Send(dataFrame([]byte("advertisement"))); err != nil {
					return err
				}
				<-stream.Context().Done()
				return stream.Context().Err()
			})
			defer stop()
			if _, err := relay(context.Background(), opener, testRequest(UploadPack), streams.input, streams.output); err == nil {
				t.Fatal("relay accepted local I/O failure")
			}
			assertChannelClosed(t, serverDone, "server disconnect")
		})
	}
}

func TestRelayEnforcesCumulativeLocalByteLimits(t *testing.T) {
	t.Run("input", func(t *testing.T) {
		opener, stop := startRelayService(t, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
			_, _ = stream.Recv()
			if err := stream.Send(dataFrame([]byte("a"))); err != nil {
				return err
			}
			<-stream.Context().Done()
			return stream.Context().Err()
		})
		defer stop()
		limits := relayLimits{chunkBytes: 4, maxBytes: 4}
		if _, err := relayWithLimits(context.Background(), opener, testRequest(UploadPack), bytes.NewReader([]byte("12345")), io.Discard, limits); err == nil {
			t.Fatal("relay accepted input above local limit")
		}
	})
	t.Run("output", func(t *testing.T) {
		opener, stop := startRelayService(t, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
			_, _ = stream.Recv()
			return stream.Send(dataFrame([]byte("12345")))
		})
		defer stop()
		limits := relayLimits{chunkBytes: 8, maxBytes: 4}
		if _, err := relayWithLimits(context.Background(), opener, testRequest(UploadPack), bytes.NewReader(nil), io.Discard, limits); err == nil {
			t.Fatal("relay accepted output above local limit")
		}
	})
}

func TestRunUsesSharedTLSAndBearerTransport(t *testing.T) {
	input := bytes.Repeat([]byte("request"), 20_000)
	var received bytes.Buffer
	startTLSGitService(t, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
		values := metadata.ValueFromIncomingContext(stream.Context(), "authorization")
		if len(values) != 1 || values[0] != "Bearer "+testGitToken {
			return status.Error(codes.Unauthenticated, "missing token")
		}
		open, err := stream.Recv()
		if err != nil || open.GetOpen().GetRepository().GetHost() != "git.example" {
			return status.Error(codes.InvalidArgument, "bad open")
		}
		if err := stream.Send(dataFrame([]byte("advertisement"))); err != nil {
			return err
		}
		for {
			frame, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			received.Write(frame.GetData().GetData())
		}
		return stream.Send(terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0))
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"-o", "SendEnv=GIT_PROTOCOL", "-p", "2222", "git@git.example", "git-upload-pack 'owner/repo.git'"}, bytes.NewReader(input), &stdout, &stderr)
	if code != 0 || stdout.String() != "advertisement" || stderr.Len() != 0 || !bytes.Equal(received.Bytes(), input) {
		t.Fatalf("Run()=%d stdout=%q stderr=%q received=%d", code, stdout.String(), stderr.String(), received.Len())
	}
}

func TestRunUsesOnlyFixedDiagnosticAndMapsExitCodes(t *testing.T) {
	t.Run("parse rejection before configuration", func(t *testing.T) {
		setGitEnv(t, "", "", "", "")
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"-o", "ProxyCommand=sh", "git@git.example", "git-upload-pack 'owner/repo.git'"}, bytes.NewReader(nil), &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || stderr.String() != fixedDiagnostic {
			t.Fatalf("Run()=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
	t.Run("configuration rejection", func(t *testing.T) {
		setGitEnv(t, "", "", "", "")
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"git@git.example", "git-upload-pack 'owner/repo.git'"}, bytes.NewReader(nil), &stdout, &stderr)
		if code != 1 || stdout.Len() != 0 || stderr.String() != fixedDiagnostic {
			t.Fatalf("Run()=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
	for _, test := range []struct {
		name     string
		category repowolfv1.GitTerminalCategory
		exitCode int32
		want     int
	}{
		{name: "provider status", category: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE, exitCode: 23, want: 23},
		{name: "invalid shell status", category: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE, exitCode: 300, want: 1},
		{name: "policy denial", category: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PERMISSION_DENIED, exitCode: 1, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			startTLSGitService(t, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
				_, _ = stream.Recv()
				return stream.Send(terminalFrame(test.category, test.exitCode))
			})
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"git@git.example", "git-receive-pack 'owner/repo.git'"}, bytes.NewReader([]byte("secret input")), &stdout, &stderr)
			if code != test.want || stdout.Len() != 0 || stderr.String() != fixedDiagnostic {
				t.Fatalf("Run()=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestContextStatusDistinguishesDeadlineFromSignalCancellation(t *testing.T) {
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if statusCode := contextStatus(deadline); statusCode != 1 {
		t.Fatalf("deadline status = %d", statusCode)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if statusCode := contextStatus(canceled); statusCode != 130 {
		t.Fatalf("cancellation status = %d", statusCode)
	}
}

func TestRunMapsCancellationCauseWithoutLeakingDetails(t *testing.T) {
	started := make(chan struct{})
	startTLSGitService(t, func(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}
		close(started)
		<-stream.Context().Done()
		return stream.Context().Err()
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan int, 1)
	var stderr bytes.Buffer
	go func() {
		done <- Run(ctx, []string{"git@git.example", "git-upload-pack 'owner/repo.git'"}, bytes.NewReader(nil), io.Discard, &stderr)
	}()
	assertChannelClosed(t, started, "server open")
	cancel(testSignalCause{code: 143})
	select {
	case code := <-done:
		if code != 143 || stderr.String() != fixedDiagnostic {
			t.Fatalf("Run()=%d stderr=%q", code, stderr.String())
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

const testGitToken = "rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type testSignalCause struct{ code int }

func (cause testSignalCause) Error() string { return "sensitive signal detail" }
func (cause testSignalCause) ExitCode() int { return cause.code }

func startTLSGitService(t *testing.T, handler func(grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error) {
	t.Helper()
	certificate, caPEM := newGitTLSCertificate(t, "repowolf.test")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13})))
	repowolfv1.RegisterGitServiceServer(server, &fakeGitService{handler: handler})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	setGitEnv(t, "https://"+listener.Addr().String(), testGitToken, caFile, "repowolf.test")
}

func setGitEnv(t *testing.T, endpoint, token, caFile, serverName string) {
	t.Helper()
	t.Setenv("REPOWOLF_ENDPOINT", endpoint)
	t.Setenv("REPOWOLF_TOKEN", token)
	t.Setenv("REPOWOLF_CA_FILE", caFile)
	t.Setenv("REPOWOLF_SERVER_NAME", serverName)
}

func newGitTLSCertificate(t *testing.T, serverName string) (tls.Certificate, []byte) {
	t.Helper()
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(301), Subject: pkix.Name{CommonName: "RepoWolf Git test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	_, serverKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{SerialNumber: big.NewInt(302), DNSNames: []string{serverName}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCertificate, serverKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{serverDER}, PrivateKey: serverKey}
	return certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}

func startRelayService(t *testing.T, handler func(grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error) (streamOpener, func()) {
	t.Helper()
	return startRelayServiceExpected(t, "", handler)
}

func startRelayServiceExpected(t *testing.T, expected Operation, handler func(grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error) (streamOpener, func()) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	repowolfv1.RegisterGitServiceServer(server, &fakeGitService{expected: expected, handler: handler})
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return openerFor(repowolfv1.NewGitServiceClient(connection)), func() {
		_ = connection.Close()
		server.Stop()
		_ = listener.Close()
	}
}

type fakeGitService struct {
	repowolfv1.UnimplementedGitServiceServer
	expected Operation
	handler  func(grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error
}

func (service *fakeGitService) UploadPack(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
	if service.expected == ReceivePack {
		return errors.New("called upload-pack for receive request")
	}
	return service.handler(stream)
}

func (service *fakeGitService) ReceivePack(stream grpc.BidiStreamingServer[repowolfv1.GitFrame, repowolfv1.GitFrame]) error {
	if service.expected == UploadPack {
		return errors.New("called receive-pack for upload request")
	}
	return service.handler(stream)
}

func testRequest(operation Operation) Request {
	return Request{Repository: &repowolfv1.RepositorySelector{Host: "git.example", SshPort: 2222, Owner: "owner", Name: "repo"}, Operation: operation}
}

func openFrame(request Request) *repowolfv1.GitFrame {
	return &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Open{Open: &repowolfv1.GitOpen{Repository: request.Repository}}}
}

func dataFrame(data []byte) *repowolfv1.GitFrame {
	return &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Data{Data: &repowolfv1.GitData{Data: data}}}
}

func terminalFrame(category repowolfv1.GitTerminalCategory, status int32) *repowolfv1.GitFrame {
	return &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Terminal{Terminal: &repowolfv1.GitTerminal{Category: category, ExitCode: status}}}
}

type countingReader struct {
	reader io.Reader
	reads  int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(buffer)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("sensitive read error") }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("sensitive write error") }

func assertChannelClosed[T any](t *testing.T, channel <-chan T, description string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
