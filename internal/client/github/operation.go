package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func normalizedResult(kind operationKind, response *repowolfv1.GitHubResponse) (any, error) {
	if response == nil {
		return nil, fmt.Errorf("empty GitHub response")
	}
	if !responseMatches(kind, response) {
		return nil, fmt.Errorf("GitHub response did not match request")
	}
	var value any
	switch kind {
	case operationRepositoryView:
		value = response.GetRepositoryView().GetRepository()
	case operationIssueList:
		value = response.GetIssueList().GetIssues()
	case operationIssueView:
		value = response.GetIssueView().GetIssue()
	case operationIssueCreate:
		value = response.GetIssueCreate().GetIssue()
	case operationIssueEdit:
		value = response.GetIssueEdit().GetIssue()
	case operationIssueComment:
		value = response.GetIssueComment().GetComment()
	case operationIssueClose:
		value = response.GetIssueClose().GetIssue()
	case operationIssueReopen:
		value = response.GetIssueReopen().GetIssue()
	case operationPullList:
		value = response.GetPullList().GetPulls()
	case operationPullView:
		value = response.GetPullView().GetPull()
	case operationPullCreate:
		value = response.GetPullCreate().GetPull()
	case operationPullEdit:
		value = response.GetPullEdit().GetPull()
	case operationPullComment:
		value = response.GetPullComment().GetComment()
	case operationPullClose:
		value = response.GetPullClose().GetPull()
	case operationPullReopen:
		value = response.GetPullReopen().GetPull()
	case operationPullReady:
		value = response.GetPullReady().GetPull()
	case operationPullChecks:
		value = response.GetPullChecks().GetChecks()
	case operationRunList:
		value = response.GetRunList().GetRuns()
	case operationRunView:
		value = response.GetRunView().GetRun()
	case operationStatusView:
		value = response.GetStatusView().GetStatus()
	default:
		return nil, fmt.Errorf("unsupported response operation")
	}
	if isNilResult(value) {
		return nil, fmt.Errorf("GitHub response did not match request")
	}
	return normalizeJSON(value)
}

func responseMatches(kind operationKind, response *repowolfv1.GitHubResponse) bool {
	switch kind {
	case operationRepositoryView:
		return response.GetRepositoryView() != nil
	case operationIssueList:
		return response.GetIssueList() != nil
	case operationIssueView:
		return response.GetIssueView() != nil
	case operationIssueCreate:
		return response.GetIssueCreate() != nil
	case operationIssueEdit:
		return response.GetIssueEdit() != nil
	case operationIssueComment:
		return response.GetIssueComment() != nil
	case operationIssueClose:
		return response.GetIssueClose() != nil
	case operationIssueReopen:
		return response.GetIssueReopen() != nil
	case operationPullList:
		return response.GetPullList() != nil
	case operationPullView:
		return response.GetPullView() != nil
	case operationPullCreate:
		return response.GetPullCreate() != nil
	case operationPullEdit:
		return response.GetPullEdit() != nil
	case operationPullComment:
		return response.GetPullComment() != nil
	case operationPullClose:
		return response.GetPullClose() != nil
	case operationPullReopen:
		return response.GetPullReopen() != nil
	case operationPullReady:
		return response.GetPullReady() != nil
	case operationPullChecks:
		return response.GetPullChecks() != nil
	case operationRunList:
		return response.GetRunList() != nil
	case operationRunView:
		return response.GetRunView() != nil
	case operationStatusView:
		return response.GetStatusView() != nil
	default:
		return false
	}
}

func isNilResult(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}

func normalizeJSON(value any) (any, error) {
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Slice && reflected.IsNil() {
		return []any{}, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode typed response: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode typed response: %w", err)
	}
	return camelCaseKeys(decoded), nil
}

func camelCaseKeys(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for name, item := range typed {
			result[camelCase(name)] = camelCaseKeys(item)
		}
		return result
	case []any:
		for index := range typed {
			typed[index] = camelCaseKeys(typed[index])
		}
	}
	return value
}

func camelCase(value string) string {
	parts := strings.Split(value, "_")
	for index := 1; index < len(parts); index++ {
		if parts[index] != "" {
			parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
		}
	}
	return strings.Join(parts, "")
}
