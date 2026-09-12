package gitea

import repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"

type issueFieldDescriptor struct {
	field repowolfv1.GiteaIssueField
	name  string
}

// issueFieldDescriptors is the canonical client field registry. Its order is
// the fixed detail-rendering order, with body last so multiline text remains
// unambiguous in text formats.
var issueFieldDescriptors = []issueFieldDescriptor{
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_INDEX, "index"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_STATE, "state"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_AUTHOR, "author"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_AUTHOR_ID, "author-id"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_URL, "url"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_TITLE, "title"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_CREATED, "created"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_UPDATED, "updated"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_DEADLINE, "deadline"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_ASSIGNEES, "assignees"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_MILESTONE, "milestone"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_LABELS, "labels"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_COMMENTS, "comments"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_REPO, "repo"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_OWNER, "owner"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_KIND, "kind"},
	{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_BODY, "body"},
}

var issueFieldByName, issueFieldNameByValue = buildIssueFieldLookups()
var detailOrder = buildDetailOrder()

func buildIssueFieldLookups() (map[string]repowolfv1.GiteaIssueField, map[repowolfv1.GiteaIssueField]string) {
	byName := make(map[string]repowolfv1.GiteaIssueField, len(issueFieldDescriptors))
	byValue := make(map[repowolfv1.GiteaIssueField]string, len(issueFieldDescriptors))
	for _, descriptor := range issueFieldDescriptors {
		byName[descriptor.name] = descriptor.field
		byValue[descriptor.field] = descriptor.name
	}
	return byName, byValue
}

func buildDetailOrder() []repowolfv1.GiteaIssueField {
	fields := make([]repowolfv1.GiteaIssueField, len(issueFieldDescriptors))
	for index, descriptor := range issueFieldDescriptors {
		fields[index] = descriptor.field
	}
	return fields
}
