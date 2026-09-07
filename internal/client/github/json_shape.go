package github

func githubJSONShape(kind operationKind, value any) any {
	switch kind {
	case operationRepositoryView:
		object, ok := value.(map[string]any)
		if !ok {
			return value
		}
		result := cloneObject(object)
		result["name"] = result["repository"]
		delete(result, "repository")
		return result
	case operationIssueList:
		issues, ok := value.([]any)
		if !ok {
			return value
		}
		result := make([]any, len(issues))
		for index, issue := range issues {
			result[index] = githubIssueJSONShape(issue)
		}
		return result
	case operationIssueView, operationIssueCreate, operationIssueEdit, operationIssueLabelChange, operationIssueClose, operationIssueReopen:
		return githubIssueJSONShape(value)
	default:
		return value
	}
}

func githubIssueJSONShape(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := cloneObject(object)
	if author, ok := result["author"].(string); ok {
		result["author"] = map[string]any{"login": author}
	}
	if labels, ok := result["labels"].([]any); ok {
		shaped := make([]any, len(labels))
		for index, label := range labels {
			if name, ok := label.(string); ok {
				shaped[index] = map[string]any{"name": name}
			} else {
				shaped[index] = label
			}
		}
		result["labels"] = shaped
	}
	if comments, ok := result["comments"].([]any); ok {
		shaped := make([]any, 0, len(comments))
		for _, comment := range comments {
			item, ok := comment.(map[string]any)
			if !ok {
				continue
			}
			author, _ := item["author"].(string)
			shaped = append(shaped, map[string]any{
				"author":    map[string]any{"login": author},
				"body":      item["body"],
				"createdAt": item["createdAt"],
			})
		}
		result["comments"] = shaped
	}
	return result
}

func cloneObject(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for name, item := range value {
		result[name] = item
	}
	return result
}
