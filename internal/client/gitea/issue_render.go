package gitea

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type orderedField struct {
	name    string
	value   any
	present bool
}

func renderIssueList(parsed command, result *repowolfv1.GiteaIssueListResult) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("invalid issue list")
	}
	request := parsed.request.GetIssueList()
	if request == nil || len(result.Issues) > int(request.Limit) {
		return nil, fmt.Errorf("invalid issue list")
	}
	fields := parsed.fields
	if len(fields) == 0 {
		fields = request.Fields
	}
	names := make([]string, len(fields))
	rows := make([][]orderedField, len(result.Issues))
	for i, f := range fields {
		n, e := issueFieldName(f)
		if e != nil {
			return nil, e
		}
		names[i] = n
	}
	for i, issue := range result.Issues {
		if err := validateProjectedIssue(issue, fields); err != nil {
			return nil, err
		}
		if len(issue.Comments) != 0 {
			return nil, fmt.Errorf("hydrated list comments")
		}
		row := make([]orderedField, len(fields))
		for j, f := range fields {
			v, p, e := issueFieldValue(issue, f)
			if e != nil {
				return nil, e
			}
			row[j] = orderedField{names[j], v, p}
		}
		rows[i] = row
	}
	var b bytes.Buffer
	switch parsed.format {
	case outputTable:
		b.WriteString(strings.Join(names, "\t"))
		b.WriteByte('\n')
		for _, row := range rows {
			b.WriteString(joinText(row, "\t"))
			b.WriteByte('\n')
		}
	case outputSimple:
		for _, row := range rows {
			b.WriteString(joinText(row, " "))
			b.WriteByte('\n')
		}
	case outputJSON:
		b.WriteByte('[')
		for i, row := range rows {
			if i > 0 {
				b.WriteByte(',')
			}
			v, e := marshalOrderedObject(row)
			if e != nil {
				return nil, e
			}
			b.Write(v)
		}
		b.WriteString("]\n")
	default:
		return nil, fmt.Errorf("invalid output")
	}
	return b.Bytes(), nil
}

const maximumIssueComments = 1000

var detailOrder = []repowolfv1.GiteaIssueField{1, 2, 3, 4, 5, 6, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 7}

func renderIssueView(parsed command, result *repowolfv1.GiteaIssueViewResult) ([]byte, error) {
	if result == nil || result.Issue == nil {
		return nil, fmt.Errorf("invalid issue view")
	}
	requested := parsed.request.GetIssueView().GetIncludeComments()
	if err := validateIssueRecord(result.Issue, requested); err != nil {
		return nil, err
	}
	values := make([]orderedField, 0, len(detailOrder))
	for _, field := range detailOrder {
		if field == repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_COMMENTS && !requested {
			continue
		}
		name, _ := issueFieldName(field)
		var v any
		var p bool
		var e error
		if field == repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_COMMENTS {
			v, e = renderedComments(result.Issue.Comments)
			p = true
		} else {
			v, p, e = issueFieldValue(result.Issue, field)
		}
		if e != nil {
			return nil, e
		}
		if p {
			values = append(values, orderedField{name, v, true})
		}
	}
	var b bytes.Buffer
	switch parsed.format {
	case outputJSON:
		v, e := marshalOrderedObject(values)
		if e != nil {
			return nil, e
		}
		b.Write(v)
		b.WriteByte('\n')
	case outputTable:
		names := make([]string, len(values))
		for i, v := range values {
			names[i] = v.name
		}
		b.WriteString(strings.Join(names, "\t"))
		b.WriteByte('\n')
		b.WriteString(joinText(values, "\t"))
		b.WriteByte('\n')
	case outputSimple:
		for _, v := range values {
			if v.name == "body" {
				fmt.Fprintf(&b, "body: %v\n", v.value)
				continue
			}
			if v.name == "comments" {
				b.WriteString("comments:\n")
				for _, c := range result.Issue.Comments {
					fields, e := commentFields(c)
					if e != nil {
						return nil, e
					}
					for _, f := range fields {
						if f.name == "body" {
							fmt.Fprintf(&b, "  body: %v\n", f.value)
							continue
						}
						fmt.Fprintf(&b, "  %s: %s\n", f.name, textValue(f.value))
					}
				}
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", v.name, textValue(v.value))
		}
	default:
		return nil, fmt.Errorf("invalid output")
	}
	return b.Bytes(), nil
}

func validateProjectedIssue(issue *repowolfv1.GiteaIssueRecord, fields []repowolfv1.GiteaIssueField) error {
	if issue == nil || issue.Index <= 0 || issue.State != repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN && issue.State != repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_CLOSED || issue.Kind != repowolfv1.GiteaIssueKind_GITEA_ISSUE_KIND_ISSUE {
		return fmt.Errorf("invalid issue identity")
	}
	selectedCreated, selectedUpdated := false, false
	for _, f := range fields {
		if _, _, e := issueFieldValue(issue, f); e != nil {
			return e
		}
		selectedCreated = selectedCreated || f == repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_CREATED
		selectedUpdated = selectedUpdated || f == repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_UPDATED
	}
	if selectedCreated && selectedUpdated && issue.Updated.AsTime().Before(issue.Created.AsTime()) {
		return fmt.Errorf("invalid issue timestamp order")
	}
	return nil
}
func validateIssueRecord(issue *repowolfv1.GiteaIssueRecord, commentsRequested bool) error {
	if err := validateProjectedIssue(issue, detailOrder); err != nil {
		return err
	}
	if issue.AuthorId <= 0 || issue.Author == "" || issue.Url == "" || issue.Title == "" || issue.Owner == "" || issue.Repo == "" || issue.CommentCount < 0 {
		return fmt.Errorf("invalid issue")
	}
	if !validTime(issue.Created) || !validTime(issue.Updated) || issue.Updated.AsTime().Before(issue.Created.AsTime()) || issue.Deadline != nil && !validTime(issue.Deadline) {
		return fmt.Errorf("invalid issue timestamp")
	}
	if len(issue.Comments) > maximumIssueComments {
		return fmt.Errorf("too many comments")
	}
	if !commentsRequested && len(issue.Comments) > 0 {
		return fmt.Errorf("unexpected comments")
	}
	if commentsRequested && issue.CommentCount != int64(len(issue.Comments)) {
		return fmt.Errorf("incomplete comments")
	}
	var last int64
	for _, c := range issue.Comments {
		if c == nil || c.Id <= last {
			return fmt.Errorf("invalid comment order")
		}
		if _, e := commentFields(c); e != nil {
			return e
		}
		last = c.Id
	}
	return nil
}
func validTime(v *timestamppb.Timestamp) bool { return v != nil && v.IsValid() }
func issueFieldName(f repowolfv1.GiteaIssueField) (string, error) {
	names := map[repowolfv1.GiteaIssueField]string{1: "index", 2: "state", 3: "author", 4: "author-id", 5: "url", 6: "title", 7: "body", 8: "created", 9: "updated", 10: "deadline", 11: "assignees", 12: "milestone", 13: "labels", 14: "comments", 15: "repo", 16: "owner", 17: "kind"}
	n, ok := names[f]
	if !ok {
		return "", fmt.Errorf("invalid issue field")
	}
	return n, nil
}
func issueFieldValue(i *repowolfv1.GiteaIssueRecord, f repowolfv1.GiteaIssueField) (any, bool, error) {
	switch f {
	case 1:
		if i.Index <= 0 {
			return nil, false, fmt.Errorf("invalid index")
		}
		return i.Index, true, nil
	case 2:
		if i.State == 1 {
			return "open", true, nil
		}
		if i.State == 2 {
			return "closed", true, nil
		}
		return nil, false, fmt.Errorf("invalid state")
	case 3:
		if i.Author == "" || !validOutputString(i.Author) {
			return nil, false, fmt.Errorf("invalid author")
		}
		return i.Author, true, nil
	case 4:
		if i.AuthorId <= 0 {
			return nil, false, fmt.Errorf("invalid author id")
		}
		return i.AuthorId, true, nil
	case 5:
		if i.Url == "" || !validOutputString(i.Url) {
			return nil, false, fmt.Errorf("invalid url")
		}
		return i.Url, true, nil
	case 6:
		if i.Title == "" || !validOutputString(i.Title) {
			return nil, false, fmt.Errorf("invalid title")
		}
		return i.Title, true, nil
	case 7:
		if !validOutputString(i.Body) {
			return nil, false, fmt.Errorf("invalid body")
		}
		return i.Body, true, nil
	case 8:
		if !validTime(i.Created) {
			return nil, false, fmt.Errorf("invalid created")
		}
		return i.Created.AsTime().UTC().Format(time.RFC3339), true, nil
	case 9:
		if !validTime(i.Updated) {
			return nil, false, fmt.Errorf("invalid updated")
		}
		return i.Updated.AsTime().UTC().Format(time.RFC3339), true, nil
	case 10:
		if i.Deadline == nil {
			return nil, false, nil
		}
		if !validTime(i.Deadline) {
			return nil, false, fmt.Errorf("invalid deadline")
		}
		return i.Deadline.AsTime().UTC().Format(time.RFC3339), true, nil
	case 11:
		for _, value := range i.Assignees {
			if value == "" || !validOutputString(value) {
				return nil, false, fmt.Errorf("invalid assignee")
			}
		}
		return nonnil(i.Assignees), true, nil
	case 12:
		if i.Milestone == nil {
			return nil, false, nil
		}
		if i.GetMilestone() == "" || !validOutputString(i.GetMilestone()) {
			return nil, false, fmt.Errorf("invalid milestone")
		}
		return i.GetMilestone(), true, nil
	case 13:
		for _, value := range i.Labels {
			if value == "" || !validOutputString(value) {
				return nil, false, fmt.Errorf("invalid label")
			}
		}
		return nonnil(i.Labels), true, nil
	case 14:
		if i.CommentCount < 0 {
			return nil, false, fmt.Errorf("invalid comment count")
		}
		if len(i.Comments) > 0 {
			raw := make([]json.RawMessage, len(i.Comments))
			for n, c := range i.Comments {
				fs, e := commentFields(c)
				if e != nil {
					return nil, false, e
				}
				raw[n], e = marshalOrderedObject(fs)
				if e != nil {
					return nil, false, e
				}
			}
			return raw, true, nil
		}
		return i.CommentCount, true, nil
	case 15:
		if !validPart(i.Repo) {
			return nil, false, fmt.Errorf("invalid repo")
		}
		return i.Repo, true, nil
	case 16:
		if !validPart(i.Owner) {
			return nil, false, fmt.Errorf("invalid owner")
		}
		return i.Owner, true, nil
	case 17:
		if i.Kind != 1 {
			return nil, false, fmt.Errorf("invalid kind")
		}
		return "issue", true, nil
	}
	return nil, false, fmt.Errorf("invalid field")
}
func renderedComments(comments []*repowolfv1.GiteaCommentRecord) ([]json.RawMessage, error) {
	values := make([]json.RawMessage, len(comments))
	for i, comment := range comments {
		fields, err := commentFields(comment)
		if err != nil {
			return nil, err
		}
		values[i], err = marshalOrderedObject(fields)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func commentFields(c *repowolfv1.GiteaCommentRecord) ([]orderedField, error) {
	if c == nil || c.Id <= 0 || c.AuthorId <= 0 || c.Author == "" || c.Url == "" || !validOutputString(c.Author) || !validOutputString(c.Url) || !validOutputString(c.Body) || !validTime(c.Created) || !validTime(c.Updated) || c.Updated.AsTime().Before(c.Created.AsTime()) {
		return nil, fmt.Errorf("invalid comment")
	}
	return []orderedField{{"id", c.Id, true}, {"author", c.Author, true}, {"author-id", c.AuthorId, true}, {"url", c.Url, true}, {"created", c.Created.AsTime().UTC().Format(time.RFC3339), true}, {"updated", c.Updated.AsTime().UTC().Format(time.RFC3339), true}, {"body", c.Body, true}}, nil
}
func marshalOrderedObject(fields []orderedField) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, f := range fields {
		if !f.present {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		n, _ := json.Marshal(f.name)
		v, e := json.Marshal(f.value)
		if e != nil {
			return nil, e
		}
		b.Write(n)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
func joinText(fields []orderedField, sep string) string {
	values := make([]string, len(fields))
	for i, f := range fields {
		if f.present {
			values[i] = textValue(f.value)
		}
	}
	return strings.Join(values, sep)
}
func textValue(v any) string {
	switch x := v.(type) {
	case string:
		return cell(x)
	case []string:
		return cell(strings.Join(x, ", "))
	case []json.RawMessage:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		return fmt.Sprint(x)
	}
}
func validOutputString(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}
func nonnil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
