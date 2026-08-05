package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestGitHubServiceEnforcesFinalProtobufLimitIncludingMetadata(t *testing.T) {
	const requestID = "request-id"
	request := githubServerRequest(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{Number: 1}})
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), requestID)

	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"exact limit", messageLimitBytes, false},
		{"one byte over", messageLimitBytes + 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			providerResponse := githubResponseWithFinalSize(t, test.size, requestID)
			executor := &fakeGitHubExecutor{response: providerResponse}
			service := newGitHubService(githubPolicy(t, config.IssuesRead, config.ProviderGitHub), executor, &eventSink{})

			response, err := service.Execute(ctx, request)
			if test.wantErr {
				if response != nil || !errors.Is(err, runner.ErrOutputLimit) {
					t.Fatalf("Execute() = %#v, %v, want no response and output limit", response, err)
				}
				mapped := rpcstatus.Error(err)
				if status.Code(mapped) != codes.ResourceExhausted || status.Convert(mapped).Message() != "request limit exceeded" {
					t.Fatalf("canonical error = %v", mapped)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := proto.Size(response); got != messageLimitBytes {
				t.Fatalf("final protobuf size = %d, want %d", got, messageLimitBytes)
			}
		})
	}
}

func githubResponseWithFinalSize(t *testing.T, target int, requestID string) *repowolfv1.GitHubResponse {
	t.Helper()
	body := strings.Repeat("x", target-64)
	response := &repowolfv1.GitHubResponse{
		Meta: &repowolfv1.ResponseMeta{RequestId: requestID},
		Result: &repowolfv1.GitHubResponse_IssueView{IssueView: &repowolfv1.GitHubIssueViewResult{
			Issue: &repowolfv1.GitHubIssueRecord{Body: &body},
		}},
	}
	for range 8 {
		delta := target - proto.Size(response)
		if delta == 0 {
			response.Meta = nil
			return response
		}
		if delta > 0 {
			body += strings.Repeat("x", delta)
		} else {
			body = body[:len(body)+delta]
		}
		response.GetIssueView().Issue.Body = &body
	}
	t.Fatalf("could not construct %d-byte protobuf; got %d", target, proto.Size(response))
	return nil
}
