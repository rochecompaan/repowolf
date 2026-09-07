package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

// Regression: label listing uses fixed, locally generated REST pages and stops
// only when GitHub's included response metadata reports no next page.
func TestLabelListPagination(t *testing.T) {
	t.Run("short terminal page", func(t *testing.T) {
		first := labelPage(t, 1, 100, true)
		last := labelPage(t, 101, 1, false)
		caller := &fakeCaller{results: []runner.Result{{Stdout: first}, {Stdout: last}}}

		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), labelListRequest(101))
		if err != nil {
			t.Fatal(err)
		}
		labels := response.GetLabelList().GetLabels()
		if len(labels) != 101 || labels[0].GetName() != "label-1" || labels[100].GetName() != "label-101" {
			t.Fatalf("labels = %d, first/last = %q/%q", len(labels), labels[0].GetName(), labels[100].GetName())
		}
		want := []runner.Command{
			expectedLabelPage("/repos/owner/repo/labels?page=1&per_page=100", maximumPaginatedReadBytes),
			expectedLabelPage("/repos/owner/repo/labels?page=2&per_page=1", maximumPaginatedReadBytes-len(first)),
		}
		if !reflect.DeepEqual(caller.commands, want) {
			t.Fatalf("commands = %#v\nwant %#v", caller.commands, want)
		}
	})

	t.Run("invalid label names are rejected", func(t *testing.T) {
		for _, name := range []string{"", "bad\x00name", strings.Repeat("x", 51)} {
			body, err := json.Marshal([]map[string]string{{"name": name}})
			if err != nil {
				t.Fatal(err)
			}
			caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(includedResponse(nil, string(body)))}}}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), labelListRequest(1))
			if response != nil || err == nil {
				t.Fatalf("Execute(%q) = %#v, %v, want no response and error", name, response, err)
			}
		}
	})

	t.Run("ten full pages are returned", func(t *testing.T) {
		results := make([]runner.Result, 10)
		for page := 1; page <= 10; page++ {
			results[page-1].Stdout = labelPage(t, (page-1)*100+1, 100, page < 10)
		}
		caller := &fakeCaller{results: results}

		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), labelListRequest(1000))
		if err != nil {
			t.Fatal(err)
		}
		labels := response.GetLabelList().GetLabels()
		if len(labels) != 1000 || labels[999].GetName() != "label-1000" {
			t.Fatalf("labels = %d, last = %q", len(labels), labels[len(labels)-1].GetName())
		}
		if len(caller.commands) != 10 {
			t.Fatalf("commands = %d, want 10", len(caller.commands))
		}
		for index, command := range caller.commands {
			endpoint := command.Args[len(command.Args)-1]
			want := fmt.Sprintf("/repos/owner/repo/labels?page=%d&per_page=100", index+1)
			if endpoint != want {
				t.Fatalf("command %d endpoint = %q, want %q", index+1, endpoint, want)
			}
		}
	})
}

// Regression: malformed, repeated, or contradictory included-response
// pagination metadata cannot redirect or extend label pagination.
func TestLabelListRejectsPaginationMetadata(t *testing.T) {
	validBody := `[{"name":"bug"}]`
	fullBody := labelBody(t, 1, 100)
	tests := []struct {
		name string
		raw  string
	}{
		{"malformed status", "not-http\r\n\r\n" + validBody},
		{"unsuccessful status", "HTTP/2.0 404 Not Found\r\n\r\n" + validBody},
		{"malformed header", "HTTP/2.0 200 OK\r\ninvalid-header\r\n\r\n" + validBody},
		{"conflicting link headers", includedResponse([]string{
			`Link: <https://api.github.example/labels?page=2>; rel="next"`,
			`link: <https://api.github.example/labels?page=2>; rel="next"`,
		}, validBody)},
		{"unknown relation", includedResponse([]string{`Link: <https://api.github.example/labels?page=2>; rel="later"`}, validBody)},
		{"wrong next page", includedResponse([]string{`Link: <https://github.example/repos/owner/repo/labels?page=3&per_page=100>; rel="next"`}, fullBody)},
		{"malformed relation", includedResponse([]string{`Link: https://api.github.example/labels?page=2; rel="next"`}, validBody)},
		{"duplicate relation", includedResponse([]string{`Link: <https://github.example/repos/owner/repo/labels?page=2&per_page=100>; rel="next", <https://github.example/repos/owner/repo/labels?page=2&per_page=100>; rel="next"`}, fullBody)},
		{"foreign link host", includedResponse([]string{`Link: <https://untrusted.example/repos/owner/repo/labels?page=2&per_page=100>; rel="next"`}, fullBody)},
		{"wrong link repository path", includedResponse([]string{`Link: <https://github.example/repos/other/repo/labels?page=2&per_page=100>; rel="next"`}, fullBody)},
		{"missing link per page", includedResponse([]string{`Link: <https://github.example/repos/owner/repo/labels?page=2>; rel="next"`}, fullBody)},
		{"wrong link per page", includedResponse([]string{`Link: <https://github.example/repos/owner/repo/labels?page=2&per_page=99>; rel="next"`}, fullBody)},
		{"extra link query", includedResponse([]string{`Link: <https://github.example/repos/owner/repo/labels?page=2&per_page=100&token=bad>; rel="next"`}, fullBody)},
		{"short body advertises next", includedResponse([]string{`Link: <https://github.example/repos/owner/repo/labels?page=2&per_page=100>; rel="next"`}, validBody)},
		{"content length trailing data", framedIncludedResponse(t, validBody, validBody)},
		{"chunked trailing data", chunkedIncludedResponse(validBody, validBody)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(test.raw)}}}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), labelListRequest(200))
			if response != nil || err == nil {
				t.Fatalf("Execute() = %#v, %v, want no response and error", response, err)
			}
			if len(caller.commands) != 1 {
				t.Fatalf("commands = %d, want 1", len(caller.commands))
			}
		})
	}
}

// Regression: label pagination fails closed at either the requested record
// bound or the ten-page provider-call bound without returning partial data.
func TestLabelListOverflow(t *testing.T) {
	t.Run("page ten advertises next", func(t *testing.T) {
		results := make([]runner.Result, 10)
		for page := 1; page <= 10; page++ {
			results[page-1].Stdout = labelPage(t, (page-1)*100+1, 100, true)
		}
		caller := &fakeCaller{results: results}

		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), labelListRequest(1000))
		if response != nil || !errors.Is(err, runner.ErrOutputLimit) {
			t.Fatalf("Execute() = %#v, %v, want output limit", response, err)
		}
		if len(caller.commands) != 10 {
			t.Fatalf("commands = %d, want no eleventh call", len(caller.commands))
		}
	})

	t.Run("page returns more than requested", func(t *testing.T) {
		caller := &fakeCaller{results: []runner.Result{{Stdout: labelPage(t, 1, 2, false)}}}
		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), labelListRequest(1))
		if response != nil || !errors.Is(err, runner.ErrOutputLimit) {
			t.Fatalf("Execute() = %#v, %v, want output limit", response, err)
		}
	})
}

func labelListRequest(limit uint64) *repowolfv1.GitHubRequest {
	return &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelList{LabelList: &repowolfv1.GitHubLabelListRequest{Limit: limit}}}
}

func labelPage(t *testing.T, first, count int, hasNext bool) []byte {
	t.Helper()
	body := labelBody(t, first, count)
	var headers []string
	if hasNext {
		page := (first-1)/100 + 2
		headers = []string{fmt.Sprintf(`lInK: <https://github.example/repos/owner/repo/labels?page=%d&per_page=%d>; rel="next"`, page, count)}
	}
	return []byte(includedResponse(headers, body))
}

func labelBody(t *testing.T, first, count int) string {
	t.Helper()
	labels := make([]map[string]string, count)
	for index := range labels {
		labels[index] = map[string]string{"name": fmt.Sprintf("label-%d", first+index)}
	}
	body, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func includedResponse(headers []string, body string) string {
	return "HTTP/2.0 200 OK\r\n" + strings.Join(headers, "\r\n") + "\r\n\r\n" + body
}

func framedIncludedResponse(t *testing.T, body, trailing string) string {
	t.Helper()
	return fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n%s%s", len(body), body, trailing)
}

func chunkedIncludedResponse(body, trailing string) string {
	return fmt.Sprintf("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n0\r\n\r\n%s", len(body), body, trailing)
}

func expectedLabelPage(endpoint string, stdoutLimit int) runner.Command {
	command := expectedAPI("GET", endpoint, nil, stdoutLimit)
	command.Args = append([]string{command.Args[0], "--include"}, command.Args[1:]...)
	return command
}
