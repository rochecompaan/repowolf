package gitea

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPullPresencePreservesAbsentNullFalseTrue(t *testing.T) {
	ctx, collector := withPullPresenceCollector(context.Background())
	transport := &pullPresenceTransport{base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"number":1},{"number":2,"mergeable":null},{"number":3,"mergeable":false,"allow_maintainer_edit":true}]`)), Request: request}, nil
	})}
	response, err := transport.RoundTrip((&http.Request{}).WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	values, err := collector.finish(pullPresenceArray)
	if err != nil {
		t.Fatal(err)
	}
	if values[1].mergeable != nil || values[2].mergeable != nil || values[3].mergeable == nil || *values[3].mergeable || values[3].allowMaintainerEdit == nil || !*values[3].allowMaintainerEdit {
		t.Fatalf("unexpected values %#v", values)
	}
}

func TestPullPresenceCloseDrainsTrailingResponseBytes(t *testing.T) {
	ctx, collector := withPullPresenceCollector(context.Background())
	body := &chunkReadCloser{chunks: [][]byte{[]byte(`{"number":1}`), []byte(` trailing`)}}
	transport := &pullPresenceTransport{base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Request: request}, nil
	})}
	response, err := transport.RoundTrip((&http.Request{}).WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len(`{"number":1}`))
	if _, err := io.ReadFull(response.Body, buffer); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !body.closed {
		t.Fatal("underlying body was not closed")
	}
	if _, err := collector.finish(pullPresenceObject); err == nil {
		t.Fatal("accepted trailing bytes that arrived after the SDK stopped decoding")
	}
}

func TestPullPresenceRejectsMalformedEvidence(t *testing.T) {
	for _, body := range []string{`[{"number":1,"number":2}]`, `[{"number":0}]`, `[{"number":1,"mergeable":"false"}]`, `[{"number":1}] trailing`, `{}`} {
		collector := &pullPresenceCollector{}
		collector.write([]byte(body))
		if _, err := collector.finish(pullPresenceArray); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

type chunkReadCloser struct {
	chunks [][]byte
	closed bool
}

func (r *chunkReadCloser) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	chunk := r.chunks[0]
	r.chunks = r.chunks[1:]
	return copy(p, chunk), nil
}
func (r *chunkReadCloser) Close() error {
	r.closed = true
	return nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
