package audit

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriterSerializesUnknownAndTransitioned(t *testing.T) {
	var output bytes.Buffer
	writer := NewWriter(&output)
	value := false
	if err := writer.Write(Event{Operation: "gitea.issue_close", Outcome: OutcomeUnknown, Transitioned: &value}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "\"outcome\":\"unknown\"") || !strings.Contains(text, "\"transitioned\":false") {
		t.Fatalf("event=%s", text)
	}
}
