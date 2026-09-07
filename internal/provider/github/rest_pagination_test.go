package github

import (
	"net/url"
	"strconv"
	"testing"

	"github.com/rochecompaan/repowolf/internal/runner"
)

// Model REST offsets from the actual request, not the invocation index.
func restPageWindow(t *testing.T, command runner.Command, total int) (page, perPage, start, count int) {
	t.Helper()
	endpoint, err := url.Parse(command.Args[len(command.Args)-1])
	if err != nil {
		t.Fatal(err)
	}
	page, err = strconv.Atoi(endpoint.Query().Get("page"))
	if err != nil || page < 1 {
		t.Fatalf("invalid page: %v", command.Args)
	}
	perPage, err = strconv.Atoi(endpoint.Query().Get("per_page"))
	if err != nil || perPage < 1 {
		t.Fatalf("invalid per_page: %v", command.Args)
	}
	start = (page - 1) * perPage
	count = max(0, min(perPage, total-start))
	return
}
