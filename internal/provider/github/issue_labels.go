package github

import (
	"context"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

func changedLabels(current, add, remove []string) ([]string, error) {
	seen := make(map[string]struct{}, len(current)+len(add))
	for _, label := range current {
		if _, duplicate := seen[label]; duplicate {
			return nil, providerResponse(nil, "unique issue labels")
		}
		seen[label] = struct{}{}
	}

	removed := make(map[string]struct{}, len(remove))
	for _, label := range remove {
		removed[label] = struct{}{}
	}
	changed := make([]string, 0, len(current)+len(add))
	for _, label := range current {
		if _, drop := removed[label]; !drop {
			changed = append(changed, label)
		}
	}
	for _, label := range add {
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		changed = append(changed, label)
	}
	if len(changed) > 100 {
		return nil, providerResponse(nil, "issue label limit")
	}
	return changed, nil
}

func (adapter *Adapter) executeIssueLabelChange(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	operation := request.GetIssueLabelChange()
	budget := &aggregateBudget{limit: maximumMutationBytes}
	preflight, err := adapter.callBudgeted(ctx, adapter.issueViewCommand(repository, operation.Number, maximumMutationBytes), budget)
	if err != nil {
		return nil, err
	}

	var issue apiIssue
	if err := decode(preflight.Stdout, &issue); err != nil {
		return nil, err
	}
	if issue.PullRequest != nil {
		return nil, providerResponse(nil, "issue")
	}
	current, err := labelNames(issue.Labels)
	if err != nil {
		return nil, err
	}
	if err := labels(current); err != nil {
		return nil, providerResponse(nil, "valid issue labels")
	}
	changed, err := changedLabels(current, operation.AddLabels, operation.RemoveLabels)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	base := "/repos/" + repository.Repository.Owner + "/" + repository.Repository.Name
	command := adapter.apiCommand(repository.Provider.APIHost, "PATCH", base+"/issues/"+decimal(operation.Number), map[string]any{"labels": changed}, maximumMutationBytes)
	result, err := adapter.callBudgeted(ctx, command, budget)
	if err != nil {
		return nil, err
	}
	response, err := normalizeResolved(repository, request, "issue", result.Stdout)
	if err != nil {
		return nil, err
	}
	if proto.Size(response) > maximumResponseBytes {
		return nil, runner.ErrOutputLimit
	}
	return response, nil
}
