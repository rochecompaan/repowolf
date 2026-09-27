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

func TestPullPresenceRejectsMalformedEvidence(t *testing.T) {
	for _, body := range []string{`[{"number":1,"number":2}]`, `[{"number":0}]`, `[{"number":1,"mergeable":"false"}]`, `[{"number":1}] trailing`, `{}`} {
		collector := &pullPresenceCollector{}
		collector.write([]byte(body))
		if _, err := collector.finish(pullPresenceArray); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
