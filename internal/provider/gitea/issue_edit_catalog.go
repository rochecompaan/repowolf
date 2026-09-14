package gitea

import (
	"context"

	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

func (a *RepositoryAdapter) collectEditCatalogs(ctx context.Context, owner, repo string, intent editIntent, issue *normalizedIssue) (editCatalogs, error) {
	catalogs := editCatalogs{assignees: map[string]int64{}, labels: map[string]int64{}}
	if (intent.assigneeMode == editSet || intent.assigneeMode == editAdd) && len(intent.assignees) > 0 {
		if err := ctx.Err(); err != nil {
			return editCatalogs{}, err
		}
		users, err := a.api.GetAssignees(ctx, owner, repo)
		if err != nil {
			return editCatalogs{}, classifyProviderError(ctx, err)
		}
		ids := map[int64]bool{}
		for _, user := range users {
			if user == nil || user.ID <= 0 || user.UserName == "" || !validProviderString(user.UserName) || ids[user.ID] {
				return editCatalogs{}, rpcstatus.ErrProviderFailure
			}
			if _, exists := catalogs.assignees[user.UserName]; exists {
				return editCatalogs{}, rpcstatus.ErrProviderFailure
			}
			ids[user.ID], catalogs.assignees[user.UserName] = true, user.ID
		}
		for _, name := range intent.assignees {
			if catalogs.assignees[name] <= 0 {
				return editCatalogs{}, rpcstatus.ErrFailedPrecondition
			}
		}
	}
	neededLabels := neededLabelNames(intent, issue)
	if len(neededLabels) > 0 {
		ids, err := a.resolveLabelIDs(ctx, owner, repo, neededLabels)
		if err != nil {
			return editCatalogs{}, err
		}
		for i, name := range neededLabels {
			catalogs.labels[name] = ids[i]
		}
	}
	return catalogs, nil
}

func neededLabelNames(intent editIntent, issue *normalizedIssue) []string {
	current := nameSet(issue.labels)
	if intent.labelMode == editRemove {
		withoutLabels := intent
		withoutLabels.labelMode, withoutLabels.labels = editNone, nil
		mayReconcile := !editSatisfied(withoutLabels, issue)
		for _, name := range intent.labels {
			mayReconcile = mayReconcile || current[name]
		}
		if mayReconcile {
			return append([]string(nil), intent.labels...)
		}
		return nil
	}
	out := make([]string, 0, len(intent.labels))
	for _, name := range intent.labels {
		if intent.labelMode == editAdd && !current[name] {
			out = append(out, name)
		}
	}
	return out
}
