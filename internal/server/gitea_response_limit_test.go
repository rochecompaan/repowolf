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

func giteaResponseWithFinalSize(t *testing.T, target int, requestID string) *repowolfv1.GiteaResponse {
	t.Helper()
	description := strings.Repeat("x", target-128)
	response := &repowolfv1.GiteaResponse{
		Meta: &repowolfv1.ResponseMeta{RequestId: requestID},
		Result: &repowolfv1.GiteaResponse_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewResult{
			Repository: &repowolfv1.GiteaRepositoryRecord{Description: description},
		}},
	}
	for range 8 {
		delta := target - proto.Size(response)
		if delta == 0 {
			response.Meta = nil
			return response
		}
		if delta > 0 {
			description += strings.Repeat("x", delta)
		} else {
			description = description[:len(description)+delta]
		}
		response.GetRepositoryView().Repository.Description = description
	}
	t.Fatalf("could not construct %d-byte protobuf; got %d", target, proto.Size(response))
	return nil
}
