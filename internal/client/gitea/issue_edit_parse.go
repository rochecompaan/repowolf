package gitea

import (
	"fmt"
	"strings"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func parseIssueEdit(args []string) (command, error) {
	if len(args) == 0 {
		return command{}, fmt.Errorf("missing issue index")
	}
	index, err := parsePositive(args[0], int64(^uint64(0)>>1))
	if err != nil {
		return command{}, fmt.Errorf("invalid issue index")
	}
	request := &repowolfv1.GiteaIssueEditRequest{Index: index}
	format := outputSimple
	var repo string
	seen := map[string]bool{}
	lists := map[string][]string{}
	aliases := map[string]string{
		"-r": "repo", "--repo": "repo", "-o": "output", "--output": "output",
		"-t": "title", "--title": "title", "-d": "description", "--description": "description",
		"--set-assignees": "set-assignees", "-a": "add-assignees", "--add-assignees": "add-assignees", "--remove-assignees": "remove-assignees",
		"-L": "add-labels", "--add-labels": "add-labels", "--remove-labels": "remove-labels",
	}
	for i := 1; i < len(args); i += 2 {
		if i+1 >= len(args) || strings.Contains(args[i], "=") {
			return command{}, fmt.Errorf("invalid flag")
		}
		key, ok := aliases[args[i]]
		if !ok || seen[key] {
			return command{}, fmt.Errorf("unsupported or duplicate flag")
		}
		seen[key] = true
		value := args[i+1]
		switch key {
		case "repo":
			repo = value
		case "output":
			format, err = parseOutput(value, outputSimple)
			if err != nil {
				return command{}, err
			}
		case "title":
			if validateMutationText(value, false, 0, 255) != nil {
				return command{}, fmt.Errorf("invalid title")
			}
			v := value
			request.Title = &v
		case "description":
			if validateMutationText(value, true, 64<<10, 0) != nil {
				return command{}, fmt.Errorf("invalid description")
			}
			v := value
			request.Description = &v
		default:
			values, listErr := parseEditCSV(value, key == "set-assignees")
			if listErr != nil {
				return command{}, listErr
			}
			lists[key] = values
		}
	}
	if !seen["repo"] {
		return command{}, fmt.Errorf("missing repository")
	}
	owner, name, ok := parseSlug(repo)
	if !ok {
		return command{}, fmt.Errorf("invalid repository")
	}
	if values, ok := lists["set-assignees"]; ok {
		request.AssigneeAction = &repowolfv1.GiteaIssueEditRequest_SetAssignees{SetAssignees: &repowolfv1.GiteaStringList{Values: values}}
	} else if values, ok := lists["add-assignees"]; ok {
		request.AssigneeAction = &repowolfv1.GiteaIssueEditRequest_AddAssignees{AddAssignees: &repowolfv1.GiteaStringList{Values: values}}
	} else if values, ok := lists["remove-assignees"]; ok {
		request.AssigneeAction = &repowolfv1.GiteaIssueEditRequest_RemoveAssignees{RemoveAssignees: &repowolfv1.GiteaStringList{Values: values}}
	}
	if values, ok := lists["add-labels"]; ok {
		request.LabelAction = &repowolfv1.GiteaIssueEditRequest_AddLabels{AddLabels: &repowolfv1.GiteaStringList{Values: values}}
	} else if values, ok := lists["remove-labels"]; ok {
		request.LabelAction = &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{Values: values}}
	}
	if request.Title == nil && request.Description == nil && request.AssigneeAction == nil && request.LabelAction == nil {
		return command{}, fmt.Errorf("missing mutation")
	}
	outer := requestFor(owner, name)
	outer.Operation = &repowolfv1.GiteaRequest_IssueEdit{IssueEdit: request}
	return command{request: outer, format: format, mutation: true}, nil
}

func parseEditCSV(value string, allowEmpty bool) ([]string, error) {
	if value == "" && allowEmpty {
		return []string{}, nil
	}
	return parseUniqueCSV(value)
}
