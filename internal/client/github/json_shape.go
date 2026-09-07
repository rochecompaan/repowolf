package github

func shapeJSON(kind operationKind, value any) any {
	if kind != operationRepositoryView {
		return value
	}
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	object["name"] = object["repository"]
	delete(object, "repository")
	return object
}
