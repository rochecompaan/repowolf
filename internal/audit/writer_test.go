package audit_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
)

func TestWriterSerializesConcurrentEventsAsJSONLines(t *testing.T) {
	var output bytes.Buffer
	writer := audit.NewWriter(&output)
	const count = 100
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if err := writer.Write(audit.Event{RequestID: fmt.Sprintf("request-%d", index), Principal: "agent", Operation: "issue.create", Outcome: audit.OutcomeCompleted}); err != nil {
				t.Errorf("Write() error = %v", err)
			}
		}(index)
	}
	group.Wait()

	seen := make(map[string]bool, count)
	scanner := bufio.NewScanner(bytes.NewReader(output.Bytes()))
	for scanner.Scan() {
		var value map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatalf("invalid JSON line %q: %v", scanner.Text(), err)
		}
		seen[value["request_id"].(string)] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != count {
		t.Fatalf("JSON records = %d, want %d", len(seen), count)
	}
}

func TestAuditNeverSerializesSensitiveFields(t *testing.T) {
	event := audit.Event{
		Timestamp:   time.Unix(1, 0).UTC(),
		RequestID:   "request",
		Principal:   "agent",
		Provider:    "github",
		Repository:  "project",
		Operation:   "issue.create",
		Outcome:     audit.OutcomeCompleted,
		Reason:      "ok",
		DurationMS:  1,
		InputBytes:  2,
		OutputBytes: 3,
		Refs:        []string{"refs/heads/topic"},
		UpdateCount: 1,
	}
	var output bytes.Buffer
	if err := audit.NewWriter(&output).Write(event); err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(output.String())
	for _, forbidden := range []string{"token", "body", "comment", "pack", "argv", "stdout", "stderr", "environment", "private_key"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("audit contains forbidden field %q: %s", forbidden, output.String())
		}
	}
	var value map[string]any
	if err := json.Unmarshal(output.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"timestamp": true, "request_id": true, "principal": true, "provider": true,
		"repository": true, "operation": true, "outcome": true, "reason": true,
		"duration_ms": true, "input_bytes": true, "output_bytes": true,
		"refs": true, "update_count": true,
	}
	for field := range value {
		if !allowed[field] {
			t.Fatalf("unexpected audit field %q", field)
		}
	}
}

func TestWriterReportsSinkFailureWithoutIncludingEventData(t *testing.T) {
	secret := "sensitive-principal"
	err := audit.NewWriter(errorWriter{}).Write(audit.Event{Principal: secret})
	if err == nil || strings.Contains(err.Error(), secret) || !errors.Is(err, audit.ErrSink) {
		t.Fatalf("error = %v", err)
	}
}

func TestFlushIfIdleDoesNotWaitForBlockedAuditOutput(t *testing.T) {
	output := &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	writer := audit.NewWriter(output)
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writer.Write(audit.Event{Operation: "test", Outcome: audit.OutcomeCompleted})
	}()
	<-output.entered
	flushDone := make(chan error, 1)
	go func() { flushDone <- writer.FlushIfIdle() }()
	select {
	case err := <-flushDone:
		if !errors.Is(err, audit.ErrSink) {
			t.Fatalf("FlushIfIdle() error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		close(output.release)
		t.Fatal("FlushIfIdle waited for blocked output")
	}
	close(output.release)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
}

type blockingWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (writer *blockingWriter) Write(payload []byte) (int, error) {
	close(writer.entered)
	<-writer.release
	return len(payload), nil
}

func (*blockingWriter) Flush() error { return nil }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("unsafe sink detail") }
