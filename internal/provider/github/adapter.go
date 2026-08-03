package github

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

const maximumResponseBytes = 1 << 20

type Caller interface {
	Call(context.Context, runner.Command) (runner.Result, error)
}

type AdapterOptions struct {
	Path        string
	Environment []string
	Timeout     time.Duration
	Caller      Caller
}

// Adapter converts validated typed requests into pinned GitHub CLI calls.
type Adapter struct {
	path        string
	environment []string
	timeout     time.Duration
	caller      Caller
}

// New constructs an immutable adapter around a pinned executable and runner.
func New(options AdapterOptions) (*Adapter, error) {
	if !filepath.IsAbs(options.Path) || options.Timeout <= 0 || options.Caller == nil {
		return nil, fmt.Errorf("invalid GitHub adapter options")
	}
	return &Adapter{path: options.Path, environment: append([]string{}, options.Environment...), timeout: options.Timeout, caller: options.Caller}, nil
}

// Execute validates before any provider call and returns only normalized data.
func (adapter *Adapter) Execute(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	if adapter == nil || adapter.caller == nil {
		return nil, fmt.Errorf("GitHub adapter unavailable")
	}
	if err := ValidateGitHubRequest(request); err != nil {
		return nil, err
	}
	if repository.Provider.Kind != config.ProviderGitHub {
		return nil, policy.ErrDenied
	}
	if _, ok := request.Operation.(*repowolfv1.GitHubRequest_PullChecks); ok {
		return adapter.executeChecks(ctx, repository, request)
	}
	if _, ok := request.Operation.(*repowolfv1.GitHubRequest_PullReady); ok {
		return adapter.executeReady(ctx, repository, request)
	}
	if requiresPreflight(request) {
		if err := adapter.preflight(ctx, repository, request); err != nil {
			return nil, err
		}
	}
	plan, err := adapter.plan(repository, request)
	if err != nil {
		return nil, err
	}
	result, err := adapter.call(ctx, plan.command)
	if err != nil {
		return nil, err
	}
	if _, ok := request.Operation.(*repowolfv1.GitHubRequest_IssueView); ok {
		if err := rejectPullIssue(result.Stdout); err != nil {
			return nil, err
		}
	}
	response, err := normalize(request, plan.normalize, result.Stdout)
	if err != nil {
		return nil, err
	}
	if proto.Size(response) > maximumResponseBytes {
		return nil, runner.ErrOutputLimit
	}
	return response, nil
}

func (adapter *Adapter) call(ctx context.Context, command runner.Command) (runner.Result, error) {
	result, err := adapter.caller.Call(ctx, command)
	if err != nil {
		return result, err
	}
	if len(result.Stdout) > command.StdoutLimit {
		return result, runner.ErrOutputLimit
	}
	return result, nil
}
