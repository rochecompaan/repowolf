package gitea

import (
	"fmt"
	"sort"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

type editCollectionMode uint8

const (
	editNone editCollectionMode = iota
	editSet
	editAdd
	editRemove
)

type editIntent struct {
	title, body  *string
	assigneeMode editCollectionMode
	assignees    []string
	labelMode    editCollectionMode
	labels       []string
}

type editCatalogs struct {
	assignees map[string]int64
	labels    map[string]int64
}

type issueTextEdit struct {
	title string
	body  *string
}
type issueEditPlan struct {
	text            *issueTextEdit
	removeAssignees []string
	addAssignees    []string
	addLabelIDs     []int64
	removeLabelIDs  []int64
}

func normalizeEditIntent(request *repowolfv1.GiteaIssueEditRequest) (editIntent, error) {
	if err := validateIssueEdit(request); err != nil {
		return editIntent{}, err
	}
	intent := editIntent{title: copyString(request.Title), body: copyString(request.Description)}
	switch action := request.AssigneeAction.(type) {
	case *repowolfv1.GiteaIssueEditRequest_SetAssignees:
		intent.assigneeMode, intent.assignees = editSet, append([]string(nil), action.SetAssignees.Values...)
	case *repowolfv1.GiteaIssueEditRequest_AddAssignees:
		intent.assigneeMode, intent.assignees = editAdd, append([]string(nil), action.AddAssignees.Values...)
	case *repowolfv1.GiteaIssueEditRequest_RemoveAssignees:
		intent.assigneeMode, intent.assignees = editRemove, append([]string(nil), action.RemoveAssignees.Values...)
	}
	switch action := request.LabelAction.(type) {
	case *repowolfv1.GiteaIssueEditRequest_AddLabels:
		intent.labelMode, intent.labels = editAdd, append([]string(nil), action.AddLabels.Values...)
	case *repowolfv1.GiteaIssueEditRequest_RemoveLabels:
		intent.labelMode, intent.labels = editRemove, append([]string(nil), action.RemoveLabels.Values...)
	}
	return intent, nil
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
func nameSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func editSatisfied(intent editIntent, issue *normalizedIssue) bool {
	if issue == nil || intent.title != nil && issue.title != *intent.title || intent.body != nil && issue.body != *intent.body {
		return false
	}
	currentAssignees, requestedAssignees := nameSet(issue.assignees), nameSet(intent.assignees)
	switch intent.assigneeMode {
	case editSet:
		if len(currentAssignees) != len(requestedAssignees) {
			return false
		}
		for name := range requestedAssignees {
			if !currentAssignees[name] {
				return false
			}
		}
	case editAdd:
		for name := range requestedAssignees {
			if !currentAssignees[name] {
				return false
			}
		}
	case editRemove:
		for name := range requestedAssignees {
			if currentAssignees[name] {
				return false
			}
		}
	}
	currentLabels, requestedLabels := nameSet(issue.labels), nameSet(intent.labels)
	if intent.labelMode == editAdd {
		for name := range requestedLabels {
			if !currentLabels[name] {
				return false
			}
		}
	}
	if intent.labelMode == editRemove {
		for name := range requestedLabels {
			if currentLabels[name] {
				return false
			}
		}
	}
	return true
}

func planIssueEdit(intent editIntent, issue *normalizedIssue, catalogs editCatalogs) (issueEditPlan, error) {
	if issue == nil {
		return issueEditPlan{}, fmt.Errorf("invalid issue")
	}
	plan := issueEditPlan{}
	if intent.title != nil && issue.title != *intent.title || intent.body != nil && issue.body != *intent.body {
		title := issue.title
		if intent.title != nil {
			title = *intent.title
		}
		plan.text = &issueTextEdit{title: title, body: copyString(intent.body)}
	}
	current := nameSet(issue.assignees)
	target := nameSet(intent.assignees)
	switch intent.assigneeMode {
	case editSet:
		for name := range current {
			if !target[name] {
				plan.removeAssignees = append(plan.removeAssignees, name)
			}
		}
		for name := range target {
			if !current[name] {
				plan.addAssignees = append(plan.addAssignees, name)
			}
		}
	case editAdd:
		for name := range target {
			if !current[name] {
				plan.addAssignees = append(plan.addAssignees, name)
			}
		}
	case editRemove:
		for name := range target {
			if current[name] {
				plan.removeAssignees = append(plan.removeAssignees, name)
			}
		}
	}
	sort.Strings(plan.removeAssignees)
	sort.Strings(plan.addAssignees)
	current = nameSet(issue.labels)
	target = nameSet(intent.labels)
	var addNames, removeNames []string
	if intent.labelMode == editAdd {
		for name := range target {
			if !current[name] {
				addNames = append(addNames, name)
			}
		}
	}
	if intent.labelMode == editRemove {
		for name := range target {
			if current[name] {
				removeNames = append(removeNames, name)
			}
		}
	}
	sort.Strings(addNames)
	sort.Strings(removeNames)
	for _, name := range addNames {
		id := catalogs.labels[name]
		if id <= 0 {
			return issueEditPlan{}, fmt.Errorf("missing label")
		}
		plan.addLabelIDs = append(plan.addLabelIDs, id)
	}
	for _, name := range removeNames {
		id := catalogs.labels[name]
		if id <= 0 {
			return issueEditPlan{}, fmt.Errorf("missing label")
		}
		plan.removeLabelIDs = append(plan.removeLabelIDs, id)
	}
	return plan, nil
}

func (plan issueEditPlan) empty() bool {
	return plan.text == nil && len(plan.removeAssignees) == 0 && len(plan.addAssignees) == 0 && len(plan.addLabelIDs) == 0 && len(plan.removeLabelIDs) == 0
}
