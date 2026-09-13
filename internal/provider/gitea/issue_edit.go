package gitea

import (
	"context"
	"errors"
	"fmt"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/protobuf/proto"
)

type editExecution struct{ confirmed bool }

func (a *RepositoryAdapter) issueEdit(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GiteaIssueEditRequest) (*repowolfv1.GiteaResponse, error) {
	intent, err := normalizeEditIntent(request)
	if err != nil {
		return nil, err
	}
	issue, err := a.issuePreflight(ctx, repository, request.Index)
	if err != nil {
		return nil, err
	}
	owner, repo := repository.Repository.Owner, repository.Repository.Name
	catalogs, err := a.collectEditCatalogs(ctx, owner, repo, intent, issue)
	if err != nil {
		return nil, err
	}
	plan, err := planIssueEdit(intent, issue, catalogs)
	if err != nil {
		return nil, rpcstatus.ErrProviderFailure
	}
	if plan.empty() {
		recordTransition(ctx, false)
		return editResponse(issue)
	}
	facts := editExecution{}
	if err := a.executeIssueEditPlan(ctx, owner, repo, request.Index, plan, &facts); err != nil {
		return a.resolveIssueEditFailure(ctx, repository, request.Index, intent, facts, err)
	}
	final, err := a.issuePreflight(ctx, repository, request.Index)
	if err != nil {
		return nil, classifyEditKnownFailure(facts, err)
	}
	if editSatisfied(intent, final) {
		recordTransition(ctx, true)
		return editResponse(final)
	}
	corrective, err := planIssueEdit(intent, final, catalogs)
	if err != nil {
		return nil, classifyEditKnownFailure(facts, rpcstatus.ErrProviderFailure)
	}
	if err := a.executeIssueEditPlan(ctx, owner, repo, request.Index, corrective, &facts); err != nil {
		return a.resolveIssueEditFailure(ctx, repository, request.Index, intent, facts, err)
	}
	final, err = a.issuePreflight(ctx, repository, request.Index)
	if err != nil {
		return nil, classifyEditKnownFailure(facts, err)
	}
	if !editSatisfied(intent, final) {
		return nil, classifyEditKnownFailure(facts, rpcstatus.ErrFailedPrecondition)
	}
	recordTransition(ctx, true)
	return editResponse(final)
}

func (a *RepositoryAdapter) resolveIssueEditFailure(ctx context.Context, repository policy.ResolvedRepository, index int64, intent editIntent, facts editExecution, err error) (*repowolfv1.GiteaResponse, error) {
	if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) {
		return nil, classifyEditKnownFailure(facts, err)
	}
	if ctx.Err() == nil {
		issue, readErr := a.issuePreflight(ctx, repository, index)
		if readErr == nil && editSatisfied(intent, issue) {
			recordTransition(ctx, true)
			return editResponse(issue)
		}
	}
	return nil, rpcstatus.ErrWriteOutcomeUnknown
}

func classifyEditKnownFailure(facts editExecution, err error) error {
	if facts.confirmed {
		return rpcstatus.ErrEditPartial
	}
	return err
}

func (a *RepositoryAdapter) executeIssueEditPlan(ctx context.Context, owner, repo string, index int64, plan issueEditPlan, facts *editExecution) error {
	if plan.text != nil {
		option := sdk.EditIssueOption{Title: plan.text.title, Body: copyString(plan.text.body)}
		_, err := invokeWrite(ctx, func() (*sdk.Issue, error) { return a.api.EditIssue(ctx, owner, repo, index, option) }, func(value *sdk.Issue) error {
			issue, e := normalizeIssue(value, owner, repo)
			if e != nil || issue.index != index || issue.title != option.Title || option.Body != nil && issue.body != *option.Body {
				return fmt.Errorf("invalid text response")
			}
			return nil
		})
		if err != nil {
			return err
		}
		facts.confirmed = true
	}
	if len(plan.removeAssignees) > 0 {
		names := append([]string(nil), plan.removeAssignees...)
		_, err := invokeWrite(ctx, func() (*sdk.Issue, error) {
			return a.api.DeleteIssueAssignees(ctx, owner, repo, index, sdk.IssueAssigneesOption{Assignees: names})
		}, func(value *sdk.Issue) error {
			issue, e := normalizeIssue(value, owner, repo)
			if e != nil || issue.index != index {
				return fmt.Errorf("invalid assignee response")
			}
			current := nameSet(issue.assignees)
			for _, name := range names {
				if current[name] {
					return fmt.Errorf("assignee retained")
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		facts.confirmed = true
	}
	if len(plan.addAssignees) > 0 {
		names := append([]string(nil), plan.addAssignees...)
		_, err := invokeWrite(ctx, func() (*sdk.Issue, error) {
			return a.api.AddIssueAssignees(ctx, owner, repo, index, sdk.IssueAssigneesOption{Assignees: names})
		}, func(value *sdk.Issue) error {
			issue, e := normalizeIssue(value, owner, repo)
			if e != nil || issue.index != index {
				return fmt.Errorf("invalid assignee response")
			}
			current := nameSet(issue.assignees)
			for _, name := range names {
				if !current[name] {
					return fmt.Errorf("assignee absent")
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		facts.confirmed = true
	}
	if len(plan.addLabelIDs) > 0 {
		ids := append([]int64(nil), plan.addLabelIDs...)
		_, err := invokeWrite(ctx, func() ([]*sdk.Label, error) {
			return a.api.AddIssueLabels(ctx, owner, repo, index, sdk.IssueLabelsOption{Labels: ids})
		}, func(values []*sdk.Label) error { return validateEditLabels(values, ids) })
		if err != nil {
			return err
		}
		facts.confirmed = true
	}
	for _, labelID := range plan.removeLabelIDs {
		_, err := invokeWrite(ctx, func() (struct{}, error) { return struct{}{}, a.api.DeleteIssueLabel(ctx, owner, repo, index, labelID) }, func(struct{}) error { return nil })
		if err != nil {
			return err
		}
		facts.confirmed = true
	}
	return nil
}

func validateEditLabels(values []*sdk.Label, required []int64) error {
	ids, names := map[int64]bool{}, map[string]bool{}
	for _, label := range values {
		if label == nil || label.ID <= 0 || label.Name == "" || !validProviderString(label.Name) || ids[label.ID] || names[label.Name] {
			return fmt.Errorf("invalid label response")
		}
		ids[label.ID], names[label.Name] = true, true
	}
	for _, id := range required {
		if !ids[id] {
			return fmt.Errorf("missing added label")
		}
	}
	return nil
}

func editResponse(issue *normalizedIssue) (*repowolfv1.GiteaResponse, error) {
	response := &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueEdit{IssueEdit: &repowolfv1.GiteaIssueEditResult{Issue: projectIssue(issue, allIssueFields)}}}
	if proto.Size(response) > 8<<20 {
		return nil, rpcstatus.ErrResourceExhausted
	}
	return response, nil
}
