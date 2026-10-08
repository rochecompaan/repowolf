package gitea

import repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"

type pullFieldDescriptor struct {
	field repowolfv1.GiteaPullField
	name  string
}

var pullFieldDescriptors = []pullFieldDescriptor{
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_INDEX, "index"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_STATE, "state"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_AUTHOR, "author"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_AUTHOR_ID, "author-id"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_URL, "url"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_TITLE, "title"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MERGEABLE, "mergeable"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BASE, "base"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BASE_COMMIT, "base-commit"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_HEAD, "head"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_CREATED, "created"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_UPDATED, "updated"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_DEADLINE, "deadline"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_ASSIGNEES, "assignees"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MILESTONE, "milestone"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_LABELS, "labels"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_COMMENTS, "comments"},
	{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BODY, "body"},
}

var pullFieldByName, pullFieldNameByValue = buildPullFieldLookups()
var defaultPullFields = []repowolfv1.GiteaPullField{
	repowolfv1.GiteaPullField_GITEA_PULL_FIELD_INDEX,
	repowolfv1.GiteaPullField_GITEA_PULL_FIELD_TITLE,
	repowolfv1.GiteaPullField_GITEA_PULL_FIELD_STATE,
	repowolfv1.GiteaPullField_GITEA_PULL_FIELD_AUTHOR,
	repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MILESTONE,
	repowolfv1.GiteaPullField_GITEA_PULL_FIELD_UPDATED,
	repowolfv1.GiteaPullField_GITEA_PULL_FIELD_LABELS,
}

func buildPullFieldLookups() (map[string]repowolfv1.GiteaPullField, map[repowolfv1.GiteaPullField]string) {
	byName := make(map[string]repowolfv1.GiteaPullField, len(pullFieldDescriptors))
	byValue := make(map[repowolfv1.GiteaPullField]string, len(pullFieldDescriptors))
	for _, descriptor := range pullFieldDescriptors {
		byName[descriptor.name], byValue[descriptor.field] = descriptor.field, descriptor.name
	}
	return byName, byValue
}
