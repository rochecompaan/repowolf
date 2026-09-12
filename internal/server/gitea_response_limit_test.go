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

func TestGiteaServiceEnforcesFinalProtobufLimitIncludingMetadata(t *testing.T) {
	const requestID = "request-id"
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), requestID)

	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "exact limit", size: responseLimitBytes},
		{name: "one byte over", size: responseLimitBytes + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			providerResponse := giteaResponseWithFinalSize(t, test.size, requestID)
			executor := &fakeGiteaExecutor{response: providerResponse}
			service := newGiteaService(giteaPolicy(t, config.RepositoryRead, config.ProviderGitea), executor, &eventSink{})

			response, err := service.Execute(ctx, giteaRequest())
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
			if got := proto.Size(response); got != test.size {
				t.Fatalf("final protobuf size = %d, want %d", got, test.size)
			}
		})
	}
}

func TestGiteaIssueServiceEnforcesFinalProtobufLimit(t *testing.T) {
	const requestID = "request-id"
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), requestID)
	for _, operation := range []struct {
		name    string
		request func() *repowolfv1.GiteaRequest
	}{
		{name: "list", request: giteaIssueListRequest},
		{name: "view", request: giteaIssueViewRequest},
		{name: "view-comments", request: func() *repowolfv1.GiteaRequest {
			request := giteaIssueViewRequest()
			request.GetIssueView().IncludeComments = true
			return request
		}},
	} {
		for _, size := range []int{responseLimitBytes, responseLimitBytes + 1} {
			name := "exact limit"
			if size > responseLimitBytes {
				name = "one byte over"
			}
			t.Run(operation.name+"/"+name, func(t *testing.T) {
				providerResponse := giteaIssueResponseWithFinalSize(t, size, requestID, operation.name)
				executor := &fakeGiteaExecutor{response: providerResponse}
				service := newGiteaService(giteaPolicy(t, config.IssuesRead, config.ProviderGitea), executor, &eventSink{})
				response, err := service.Execute(ctx, operation.request())
				if size > responseLimitBytes {
					if response != nil || !errors.Is(err, runner.ErrOutputLimit) {
						t.Fatalf("Execute() = %#v, %v, want no response and output limit", response, err)
					}
					return
				}
				if err != nil || proto.Size(response) != size {
					t.Fatalf("Execute() = size %d, %v, want size %d", proto.Size(response), err, size)
				}
			})
		}
	}
}

func giteaResponseWithFinalSize(t *testing.T, target int, requestID string) *repowolfv1.GiteaResponse {
	t.Helper()
	repository := &repowolfv1.GiteaRepositoryRecord{}
	response := &repowolfv1.GiteaResponse{
		Meta: &repowolfv1.ResponseMeta{RequestId: requestID},
		Result: &repowolfv1.GiteaResponse_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewResult{
			Repository: repository,
		}},
	}
	return fillGiteaResponse(t, response, target, &repository.Description)
}

func giteaIssueResponseWithFinalSize(t *testing.T, target int, requestID, operation string) *repowolfv1.GiteaResponse {
	t.Helper()
	response := &repowolfv1.GiteaResponse{Meta: &repowolfv1.ResponseMeta{RequestId: requestID}}
	var body *string
	if operation == "list" {
		record := &repowolfv1.GiteaIssueRecord{}
		response.Result = &repowolfv1.GiteaResponse_IssueList{IssueList: &repowolfv1.GiteaIssueListResult{Issues: []*repowolfv1.GiteaIssueRecord{record}}}
		body = &record.Body
	} else {
		record := &repowolfv1.GiteaIssueRecord{}
		response.Result = &repowolfv1.GiteaResponse_IssueView{IssueView: &repowolfv1.GiteaIssueViewResult{Issue: record}}
		if operation == "view-comments" {
			record.Comments = []*repowolfv1.GiteaCommentRecord{{Body: "first"}, {}}
			body = &record.Comments[1].Body
		} else {
			body = &record.Body
		}
	}
	return fillGiteaResponse(t, response, target, body)
}

func fillGiteaResponse(t *testing.T, response *repowolfv1.GiteaResponse, target int, value *string) *repowolfv1.GiteaResponse {
	t.Helper()
	*value = strings.Repeat("x", target-128)
	for range 8 {
		delta := target - proto.Size(response)
		if delta == 0 {
			response.Meta = nil
			return response
		}
		if delta > 0 {
			*value += strings.Repeat("x", delta)
		} else {
			*value = (*value)[:len(*value)+delta]
		}
	}
	t.Fatalf("could not construct %d-byte protobuf; got %d", target, proto.Size(response))
	return nil
}
