package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestModeForBase(t *testing.T) {
	for _, name := range []string{"gh", "repowolf-git-ssh"} {
		if mode, ok := modeForBase(name); !ok || mode != name {
			t.Fatalf("modeForBase(%q) = %q, %v", name, mode, ok)
		}
	}
	if _, ok := modeForBase("unknown"); ok {
		t.Fatal("unexpected client mode")
	}
}

func TestModeForBaseUsesExecutableBase(t *testing.T) {
	if mode, ok := modeForBase("/sandbox/bin/gh"); !ok || mode != "gh" {
		t.Fatalf("modeForBase() = %q, %v", mode, ok)
	}
}

func TestRunClientDispatchesGhCompatibilityMode(t *testing.T) {
	var stderr bytes.Buffer
	status := runClient(context.Background(), "/sandbox/bin/gh", []string{"api", "/user"}, bytes.NewReader(nil), &bytes.Buffer{}, &stderr)
	if status != 2 || stderr.String() != "gh: unsupported or invalid command\n" {
		t.Fatalf("runClient() = %d, stderr=%q", status, stderr.String())
	}
}

func TestRunClientDispatchesGitSSHCompatibilityMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := runClient(context.Background(), "/sandbox/bin/repowolf-git-ssh", []string{"sh"}, bytes.NewReader(nil), &stdout, &stderr)
	if status != 2 || stdout.Len() != 0 || stderr.String() != "repowolf git transport failed\n" {
		t.Fatalf("runClient() = %d, stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}

func TestClientPreservesSignalExitStatus(t *testing.T) {
	for _, test := range []struct {
		name   string
		signal syscall.Signal
		status int
	}{
		{name: "SIGINT", signal: syscall.SIGINT, status: 130},
		{name: "SIGTERM", signal: syscall.SIGTERM, status: 143},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr == nil {
					accepted <- connection
				}
			}()

			command := exec.Command(os.Args[0], "-test.run=TestClientSignalHelperProcess")
			command.Env = append(os.Environ(),
				"REPOWOLF_SIGNAL_HELPER=1",
				"REPOWOLF_ENDPOINT=https://"+listener.Addr().String(),
				"REPOWOLF_TOKEN=rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
				"REPOWOLF_CA_FILE=",
				"REPOWOLF_SERVER_NAME=",
			)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			var connection net.Conn
			select {
			case connection = <-accepted:
				defer connection.Close()
			case <-time.After(3 * time.Second):
				_ = command.Process.Kill()
				_ = command.Wait()
				t.Fatal("client did not reach blocking TLS dial")
			}
			if err := command.Process.Signal(test.signal); err != nil {
				t.Fatal(err)
			}
			waited := make(chan error, 1)
			go func() { waited <- command.Wait() }()
			select {
			case err := <-waited:
				exitError, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("signal exit = %v, want code %d", err, test.status)
				}
				if exitError.ExitCode() != test.status {
					t.Fatalf("signal exit code = %d, want %d", exitError.ExitCode(), test.status)
				}
				if stderr.String() != "gh: interrupted\n" {
					t.Fatalf("stderr = %q", stderr.String())
				}
			case <-time.After(3 * time.Second):
				_ = command.Process.Kill()
				_ = command.Wait()
				t.Fatal("client did not exit after signal")
			}
		})
	}
}

func TestClientSignalHelperProcess(t *testing.T) {
	if os.Getenv("REPOWOLF_SIGNAL_HELPER") != "1" {
		return
	}
	os.Exit(runMain([]string{"gh", "repo", "view", "--repo", "owner/repo"}, os.Stdin, os.Stdout, os.Stderr))
}
