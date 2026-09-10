package providerhttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRedirectsAreRejectedWithoutReplayOrDisclosure(t *testing.T) {
	const token = "redirect-secret"
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				writer.Header().Set("Location", "https://untrusted.example/private-location")
				writer.WriteHeader(status)
			}))
			defer server.Close()

			client, err := New(Options{
				Authority:        strings.TrimPrefix(server.URL, "https://"),
				Token:            token,
				Timeout:          time.Second,
				MaxResponseBytes: 1024,
				Base:             server.Client().Transport,
			})
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("not-replayed"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if !errors.Is(err, ErrRedirect) {
				t.Fatalf("Do() error = %v, want redirect", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls = %d, want 1", calls.Load())
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "private-location") || strings.Contains(err.Error(), "untrusted.example") {
				t.Fatalf("redirect error disclosed sensitive data: %v", err)
			}
		})
	}
}
