package integration_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/audit"
)

type auditRecord struct {
	event  audit.Event
	fields map[string]bool
}

type auditExpectation struct {
	operation, outcome, principal, provider, repository, reason string
	required, optional                                          []string
	refs                                                        []string
	updateCount                                                 int
	inputPositive, outputPositive                               bool
}

var auditFields = map[string]bool{
	"timestamp": true, "request_id": true, "principal": true, "provider": true,
	"repository": true, "operation": true, "outcome": true, "reason": true,
	"duration_ms": true, "input_bytes": true, "output_bytes": true,
	"refs": true, "update_count": true,
}

func parseAuditRecords(contents []byte, forbidden []string) ([]auditRecord, error) {
	for _, marker := range forbidden {
		if marker != "" && bytes.Contains(contents, []byte(marker)) {
			return nil, fmt.Errorf("audit contains a forbidden marker")
		}
	}
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	var records []auditRecord
	seen := make(map[string]bool)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("blank audit record")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			return nil, fmt.Errorf("decode audit fields: %w", err)
		}
		for field := range fields {
			if !auditFields[field] {
				return nil, fmt.Errorf("unexpected audit field")
			}
		}
		canonical, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		if seen[string(canonical)] {
			return nil, fmt.Errorf("duplicate audit record")
		}
		seen[string(canonical)] = true
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var event audit.Event
		if err := decoder.Decode(&event); err != nil {
			return nil, fmt.Errorf("decode audit event: %w", err)
		}
		recordFields := make(map[string]bool, len(fields))
		for field := range fields {
			recordFields[field] = true
		}
		records = append(records, auditRecord{event: event, fields: recordFields})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("empty audit stream")
	}
	return records, nil
}

func assertAuditInvocations(t *testing.T, contents string, expected [][]auditExpectation, forbidden []string) {
	t.Helper()
	records, err := parseAuditRecords([]byte(contents), forbidden)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, invocation := range expected {
		count += len(invocation)
	}
	if len(records) != count {
		t.Fatalf("audit record count = %d, want %d", len(records), count)
	}
	index := 0
	requestIDs := make(map[string]bool, len(expected))
	for invocationIndex, invocation := range expected {
		requestID := records[index].event.RequestID
		if requestID == "" || requestIDs[requestID] {
			t.Fatalf("invocation %d has an invalid request ID", invocationIndex)
		}
		requestIDs[requestID] = true
		for recordIndex, want := range invocation {
			record := records[index]
			index++
			if record.event.RequestID != requestID || record.event.Timestamp.IsZero() {
				t.Fatalf("invocation %d record %d has invalid identity", invocationIndex, recordIndex)
			}
			got := auditExpectation{
				operation: record.event.Operation, outcome: string(record.event.Outcome), principal: record.event.Principal,
				provider: record.event.Provider, repository: record.event.Repository, reason: record.event.Reason,
				refs: record.event.Refs, updateCount: record.event.UpdateCount,
				inputPositive: record.event.InputBytes > 0, outputPositive: record.event.OutputBytes > 0,
			}
			comparison := want
			comparison.required, comparison.optional = nil, nil
			if !reflect.DeepEqual(got, comparison) {
				t.Fatalf("invocation %d record %d does not match the expected safe audit metadata", invocationIndex, recordIndex)
			}
			allowed := make(map[string]bool, len(want.required)+len(want.optional))
			for _, field := range append(append([]string(nil), want.required...), want.optional...) {
				allowed[field] = true
			}
			for _, field := range want.required {
				if !record.fields[field] {
					t.Fatalf("invocation %d record %d missing field %q", invocationIndex, recordIndex, field)
				}
			}
			for field := range record.fields {
				if !allowed[field] {
					t.Fatalf("invocation %d record %d has unexpected field %q", invocationIndex, recordIndex, field)
				}
			}
		}
	}
}

func forgeAuditExpectations(operations ...string) [][]auditExpectation {
	acceptedFields := []string{"timestamp", "request_id", "principal", "provider", "repository", "operation", "outcome"}
	terminalFields := []string{"timestamp", "request_id", "principal", "operation", "outcome", "reason"}
	result := make([][]auditExpectation, 0, len(operations))
	for _, operation := range operations {
		result = append(result, []auditExpectation{
			{operation: operation, outcome: "accepted", principal: "agent", provider: "github", repository: "alpha", required: acceptedFields},
			{operation: "/repowolf.v1.GitHubService/Execute", outcome: "completed", principal: "agent", reason: "OK", required: terminalFields, optional: []string{"duration_ms"}},
		})
	}
	return result
}

func gitAuditExpectations(allowedRef, deniedRef string) [][]auditExpectation {
	acceptedFields := []string{"timestamp", "request_id", "principal", "provider", "repository", "operation", "outcome"}
	providerFields := append(append([]string(nil), acceptedFields...), "reason")
	streamFields := []string{"timestamp", "request_id", "principal", "operation", "outcome", "reason"}
	accepted := func(operation string) auditExpectation {
		return auditExpectation{operation: operation, outcome: "accepted", principal: "agent", provider: "github", repository: "alpha", required: acceptedFields, optional: []string{"duration_ms"}}
	}
	stream := func(operation string) auditExpectation {
		return auditExpectation{operation: operation, outcome: "completed", principal: "agent", reason: "OK", required: streamFields, optional: []string{"duration_ms"}}
	}
	return [][]auditExpectation{
		{
			accepted("git.upload-pack"),
			{operation: "git.upload-pack", outcome: "completed", principal: "agent", provider: "github", repository: "alpha", reason: "GIT_TERMINAL_CATEGORY_COMPLETED", required: append(append([]string(nil), providerFields...), "input_bytes", "output_bytes"), optional: []string{"duration_ms"}, inputPositive: true, outputPositive: true},
			stream("/repowolf.v1.GitService/UploadPack"),
		},
		{
			accepted("git.receive-pack"),
			{operation: "git.receive-pack", outcome: "completed", principal: "agent", provider: "github", repository: "alpha", reason: "GIT_TERMINAL_CATEGORY_COMPLETED", required: append(append([]string(nil), providerFields...), "input_bytes", "output_bytes", "refs", "update_count"), optional: []string{"duration_ms"}, refs: []string{allowedRef}, updateCount: 1, inputPositive: true, outputPositive: true},
			stream("/repowolf.v1.GitService/ReceivePack"),
		},
		{
			accepted("git.receive-pack"),
			{operation: "git.receive-pack", outcome: "denied", principal: "agent", provider: "github", repository: "alpha", reason: "GIT_TERMINAL_CATEGORY_INVALID_REQUEST", required: append(append([]string(nil), providerFields...), "output_bytes", "refs", "update_count"), optional: []string{"duration_ms"}, refs: []string{deniedRef}, updateCount: 1, outputPositive: true},
			stream("/repowolf.v1.GitService/ReceivePack"),
		},
	}
}

func auditLeakMarkers() []string {
	return []string{agentToken, providerCredential, providerStderr, environmentMarker, issueBodyMarker, commentMarker, argvMarker, packMarker, sshStderrMarker}
}

func TestParseAuditRecordsRejectsUnsafeJSONL(t *testing.T) {
	valid := `{"timestamp":"2026-08-01T00:00:00Z","request_id":"request-1","principal":"agent","provider":"github","repository":"alpha","operation":"github.issue_list","outcome":"accepted"}`
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "malformed", input: `{"operation":`},
		{name: "unexpected field", input: strings.Replace(valid, `"outcome":"accepted"`, `"outcome":"accepted","token":"secret"`, 1)},
		{name: "duplicate", input: valid + "\n" + valid + "\n"},
		{name: "marker", input: strings.Replace(valid, "github.issue_list", "github.task13-secret-marker", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseAuditRecords([]byte(test.input), []string{"task13-secret-marker"}); err == nil {
				t.Fatalf("parseAuditRecords accepted %s input", test.name)
			}
		})
	}
}

func TestGitAcceptedAuditDurationMSIsOptional(t *testing.T) {
	const accepted = `{"timestamp":"2026-08-01T00:00:00Z","request_id":"request-1","principal":"agent","provider":"github","repository":"alpha","operation":"git.upload-pack","outcome":"accepted","duration_ms":1}`

	assertAuditInvocations(t, accepted, [][]auditExpectation{{
		gitAuditExpectations("refs/heads/allowed", "refs/heads/denied")[0][0],
	}}, nil)
	unexpected := strings.Replace(accepted, `"duration_ms":1`, `"duration_ms":1,"secret":"unsafe"`, 1)
	if _, err := parseAuditRecords([]byte(unexpected), nil); err == nil {
		t.Fatal("parseAuditRecords accepted an unexpected field")
	}
}
