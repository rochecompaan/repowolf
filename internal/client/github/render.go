package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

const maxRenderedBytes = 1 << 20

func render(parsed command, response *repowolfv1.GitHubResponse) ([]byte, error) {
	value, err := normalizedResult(parsed.kind, response)
	if err != nil {
		return nil, err
	}
	var output []byte
	if len(parsed.fields) != 0 {
		output, err = renderJSON(value, parsed.fields)
	} else {
		output, err = renderNative(parsed.kind, value)
	}
	if err != nil {
		return nil, err
	}
	if len(output) > maxRenderedBytes {
		return nil, fmt.Errorf("rendered output exceeds client limit")
	}
	return output, nil
}

func renderJSON(value any, fields []string) ([]byte, error) {
	selectObject := func(object map[string]any) map[string]any {
		selected := make(map[string]any, len(fields))
		for _, field := range fields {
			item, exists := object[field]
			if !exists {
				item = absentJSONValue(field)
			}
			selected[field] = item
		}
		return selected
	}
	var selected any
	switch typed := value.(type) {
	case map[string]any:
		selected = selectObject(typed)
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			object, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid typed list response")
			}
			items = append(items, selectObject(object))
		}
		selected = items
	default:
		return nil, fmt.Errorf("invalid typed response")
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return nil, fmt.Errorf("encode selected response: %w", err)
	}
	return append(encoded, '\n'), nil
}

func absentJSONValue(field string) any {
	switch field {
	case "private", "draft":
		return false
	case "number", "id":
		return 0
	case "assignees", "labels", "statuses":
		return []any{}
	default:
		return nil
	}
}

func renderNative(kind operationKind, value any) ([]byte, error) {
	var output bytes.Buffer
	switch kind {
	case operationIssueList:
		err := writeTable(&output, value, []string{"number", "title", "state", "labels", "updatedAt"})
		return output.Bytes(), err
	case operationPullList:
		err := writeTable(&output, value, []string{"number", "title", "head", "state", "updatedAt"})
		return output.Bytes(), err
	case operationPullChecks:
		err := writeTable(&output, value, []string{"name", "state", "conclusion", "detailsUrl"})
		return output.Bytes(), err
	case operationRunList:
		err := writeTable(&output, value, []string{"status", "name", "workflowName", "headBranch", "event", "id"})
		return output.Bytes(), err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid typed object response")
	}
	switch kind {
	case operationRepositoryView:
		writeFields(&output, object, []fieldLabel{{"name", "nameWithOwner"}, {"description", "description"}, {"url", "url"}, {"default branch", "defaultBranch"}})
	case operationIssueView, operationIssueCreate, operationIssueEdit, operationIssueClose, operationIssueReopen:
		writeFields(&output, object, []fieldLabel{{"title", "title"}, {"state", "state"}, {"author", "author"}, {"labels", "labels"}, {"assignees", "assignees"}, {"number", "number"}, {"url", "url"}})
		writeBody(&output, object["body"])
	case operationPullView, operationPullCreate, operationPullEdit, operationPullClose, operationPullReopen, operationPullReady:
		writeFields(&output, object, []fieldLabel{{"title", "title"}, {"state", "state"}, {"author", "author"}, {"head", "head"}, {"base", "base"}, {"number", "number"}, {"url", "url"}})
		writeBody(&output, object["body"])
	case operationIssueComment, operationPullComment:
		fmt.Fprintln(&output, cell(object["url"]))
	case operationRunView:
		writeFields(&output, object, []fieldLabel{{"name", "name"}, {"workflow", "workflowName"}, {"status", "status"}, {"conclusion", "conclusion"}, {"branch", "headBranch"}, {"event", "event"}, {"id", "id"}, {"url", "url"}})
	case operationStatusView:
		writeFields(&output, object, []fieldLabel{{"state", "state"}, {"object", "objectId"}})
		if statuses, exists := object["statuses"]; exists {
			if err := writeTable(&output, statuses, []string{"name", "state", "description", "targetUrl"}); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unsupported native response")
	}
	return output.Bytes(), nil
}

type fieldLabel struct{ label, field string }

func writeFields(output *bytes.Buffer, object map[string]any, fields []fieldLabel) {
	for _, field := range fields {
		fmt.Fprintf(output, "%s:\t%s\n", field.label, cell(object[field.field]))
	}
}

func writeBody(output *bytes.Buffer, value any) {
	if body := cell(value); body != "" {
		fmt.Fprintln(output, "--")
		fmt.Fprintln(output, body)
	}
}

func writeTable(output *bytes.Buffer, value any, fields []string) error {
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("invalid typed list response")
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid typed list entry")
		}
		for index, field := range fields {
			if index != 0 {
				output.WriteByte('\t')
			}
			output.WriteString(cell(object[field]))
		}
		output.WriteByte('\n')
	}
	return nil
}

func cell(value any) string {
	var text string
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	case bool:
		text = fmt.Sprint(typed)
	case []any:
		values := make([]string, len(typed))
		for index := range typed {
			values[index] = cell(typed[index])
		}
		text = strings.Join(values, ", ")
	default:
		text = fmt.Sprint(typed)
	}
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, text)
}
