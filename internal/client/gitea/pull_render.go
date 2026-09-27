package gitea

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

const maximumPullItems = 1000

func renderPullList(parsed command, result *repowolfv1.GiteaPullListResult) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("invalid pull list")
	}
	request := parsed.request.GetPullList()
	if request == nil || len(result.Pulls) > int(request.Limit) {
		return nil, fmt.Errorf("invalid pull list")
	}
	fields := parsed.pullFields
	if len(fields) == 0 {
		fields = request.Fields
	}
	names := make([]string, len(fields))
	rows := make([][]orderedField, len(result.Pulls))
	for i, field := range fields {
		name, ok := pullFieldNameByValue[field]
		if !ok {
			return nil, fmt.Errorf("invalid pull field")
		}
		names[i] = name
	}
	for i, pull := range result.Pulls {
		if err := validateProjectedPull(pull, fields); err != nil {
			return nil, err
		}
		if pull.Draft || pull.AllowMaintainerEdit != nil || len(pull.Comments) != 0 || len(pull.RequestedReviewers) != 0 || len(pull.Reviews) != 0 {
			return nil, fmt.Errorf("hydrated pull list")
		}
		row := make([]orderedField, len(fields))
		for j, field := range fields {
			value, present, err := pullFieldValue(pull, field)
			if err != nil {
				return nil, err
			}
			row[j] = orderedField{names[j], value, present}
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
			raw, err := marshalOrderedObject(row)
			if err != nil {
				return nil, err
			}
			b.Write(raw)
		}
		b.WriteString("]\n")
	default:
		return nil, fmt.Errorf("invalid output")
	}
	return b.Bytes(), nil
}

func renderPullView(parsed command, result *repowolfv1.GiteaPullViewResult) ([]byte, error) {
	if result == nil || result.Pull == nil || parsed.request.GetPullView() == nil {
		return nil, fmt.Errorf("invalid pull view")
	}
	pull := result.Pull
	includeComments := parsed.request.GetPullView().IncludeComments
	if err := validatePullRecord(pull, includeComments); err != nil {
		return nil, err
	}
	comments, err := renderedComments(pull.Comments)
	if err != nil {
		return nil, err
	}
	actors, err := renderedActors(pull.RequestedReviewers)
	if err != nil {
		return nil, err
	}
	reviews, err := renderedReviews(pull.Reviews)
	if err != nil {
		return nil, err
	}
	values := []orderedField{
		{"index", pull.Index, true}, {"state", pullStateName(pull.State), true}, {"draft", pull.Draft, true},
		{"author", pull.Author, true}, {"author-id", pull.AuthorId, true}, {"url", pull.Url, true}, {"title", pull.Title, true},
		{"mergeable", pull.GetMergeable(), pull.Mergeable != nil}, {"allow-maintainer-edit", pull.GetAllowMaintainerEdit(), pull.AllowMaintainerEdit != nil},
		{"base", pull.Base, true}, {"base-commit", pull.BaseCommit, true}, {"head", pull.Head, true},
		{"created", pull.Created.AsTime().UTC().Format(time.RFC3339), true}, {"updated", pull.Updated.AsTime().UTC().Format(time.RFC3339), true},
		{"deadline", timeValue(pull.Deadline), pull.Deadline != nil}, {"assignees", nonnil(pull.Assignees), true}, {"milestone", pull.GetMilestone(), pull.Milestone != nil}, {"labels", nonnil(pull.Labels), true},
		{"comments", comments, includeComments}, {"requested-reviewers", actors, true}, {"reviews", reviews, true}, {"body", pull.Body, true},
	}
	var b bytes.Buffer
	switch parsed.format {
	case outputJSON:
		raw, err := marshalOrderedObject(values)
		if err != nil {
			return nil, err
		}
		b.Write(raw)
		b.WriteByte('\n')
	case outputTable:
		names := make([]string, 0, len(values))
		present := make([]orderedField, 0, len(values))
		for _, v := range values {
			if v.present {
				names = append(names, v.name)
				present = append(present, v)
			}
		}
		b.WriteString(strings.Join(names, "\t"))
		b.WriteByte('\n')
		b.WriteString(joinText(present, "\t"))
		b.WriteByte('\n')
	case outputSimple:
		for _, v := range values {
			if !v.present {
				continue
			}
			if v.name == "comments" {
				if err := writeNested(&b, "comments", pull.Comments, func(value any) ([]orderedField, error) { return commentFields(value.(*repowolfv1.GiteaCommentRecord)) }); err != nil {
					return nil, err
				}
				continue
			}
			if v.name == "requested-reviewers" {
				if err := writeNested(&b, v.name, pull.RequestedReviewers, func(value any) ([]orderedField, error) {
					return reviewActorFields(value.(*repowolfv1.GiteaReviewActor))
				}); err != nil {
					return nil, err
				}
				continue
			}
			if v.name == "reviews" {
				if err := writeNested(&b, v.name, pull.Reviews, func(value any) ([]orderedField, error) {
					return pullReviewFields(value.(*repowolfv1.GiteaPullReviewRecord))
				}); err != nil {
					return nil, err
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

func writeNested[T any](b *bytes.Buffer, name string, values []T, fields func(any) ([]orderedField, error)) error {
	b.WriteString(name + ":\n")
	for _, value := range values {
		fs, err := fields(any(value))
		if err != nil {
			return err
		}
		for _, f := range fs {
			fmt.Fprintf(b, "  %s: %s\n", f.name, textValue(f.value))
		}
	}
	return nil
}

func validateProjectedPull(pull *repowolfv1.GiteaPullRecord, fields []repowolfv1.GiteaPullField) error {
	if pull == nil || pull.Index <= 0 || pull.State != repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN && pull.State != repowolfv1.GiteaPullState_GITEA_PULL_STATE_CLOSED {
		return fmt.Errorf("invalid pull identity")
	}
	selectedCreated, selectedUpdated := false, false
	for _, field := range fields {
		if _, _, err := pullFieldValue(pull, field); err != nil {
			return err
		}
		selectedCreated = selectedCreated || field == repowolfv1.GiteaPullField_GITEA_PULL_FIELD_CREATED
		selectedUpdated = selectedUpdated || field == repowolfv1.GiteaPullField_GITEA_PULL_FIELD_UPDATED
	}
	if selectedCreated && selectedUpdated && pull.Updated.AsTime().Before(pull.Created.AsTime()) {
		return fmt.Errorf("invalid pull timestamp order")
	}
	return nil
}

func validatePullRecord(pull *repowolfv1.GiteaPullRecord, commentsRequested bool) error {
	all := make([]repowolfv1.GiteaPullField, 0, 18)
	for value := 1; value <= 18; value++ {
		all = append(all, repowolfv1.GiteaPullField(value))
	}
	if err := validateProjectedPull(pull, all); err != nil {
		return err
	}
	if pull.AuthorId <= 0 || pull.Author == "" || pull.Url == "" || pull.Title == "" || pull.Base == "" || pull.BaseCommit == "" || pull.Head == "" || pull.CommentCount < 0 || !validTime(pull.Created) || !validTime(pull.Updated) || pull.Updated.AsTime().Before(pull.Created.AsTime()) || pull.Deadline != nil && !validTime(pull.Deadline) {
		return fmt.Errorf("invalid pull")
	}
	if len(pull.Comments) > maximumPullItems || len(pull.Reviews) > maximumPullItems || !commentsRequested && len(pull.Comments) != 0 || commentsRequested && int64(len(pull.Comments)) != pull.CommentCount {
		return fmt.Errorf("invalid pull hydration")
	}
	var last int64
	for _, comment := range pull.Comments {
		if comment == nil || comment.Id <= last {
			return fmt.Errorf("invalid comment order")
		}
		if _, err := commentFields(comment); err != nil {
			return err
		}
		last = comment.Id
	}
	last = 0
	for _, actor := range pull.RequestedReviewers {
		if _, err := reviewActorFields(actor); err != nil {
			return err
		}
	}
	for _, review := range pull.Reviews {
		if review == nil || review.Id <= last {
			return fmt.Errorf("invalid review order")
		}
		if _, err := pullReviewFields(review); err != nil {
			return err
		}
		last = review.Id
	}
	return nil
}

func pullFieldValue(pull *repowolfv1.GiteaPullRecord, field repowolfv1.GiteaPullField) (any, bool, error) {
	valid := func(value string, required bool) (any, bool, error) {
		if !validOutputString(value) || required && value == "" {
			return nil, false, fmt.Errorf("invalid pull string")
		}
		return value, true, nil
	}
	switch field {
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_INDEX:
		if pull.Index <= 0 {
			return nil, false, fmt.Errorf("invalid index")
		}
		return pull.Index, true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_STATE:
		name := pullStateName(pull.State)
		if name == "" {
			return nil, false, fmt.Errorf("invalid state")
		}
		return name, true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_AUTHOR:
		return valid(pull.Author, true)
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_AUTHOR_ID:
		if pull.AuthorId <= 0 {
			return nil, false, fmt.Errorf("invalid author")
		}
		return pull.AuthorId, true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_URL:
		return valid(pull.Url, true)
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_TITLE:
		return valid(pull.Title, true)
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BODY:
		return valid(pull.Body, false)
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MERGEABLE:
		if pull.Mergeable == nil {
			return nil, false, nil
		}
		return pull.GetMergeable(), true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BASE:
		return valid(pull.Base, true)
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BASE_COMMIT:
		return valid(pull.BaseCommit, true)
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_HEAD:
		return valid(pull.Head, true)
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_CREATED:
		if !validTime(pull.Created) {
			return nil, false, fmt.Errorf("invalid created")
		}
		return timeValue(pull.Created), true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_UPDATED:
		if !validTime(pull.Updated) {
			return nil, false, fmt.Errorf("invalid updated")
		}
		return timeValue(pull.Updated), true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_DEADLINE:
		if pull.Deadline == nil {
			return nil, false, nil
		}
		if !validTime(pull.Deadline) {
			return nil, false, fmt.Errorf("invalid deadline")
		}
		return timeValue(pull.Deadline), true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_ASSIGNEES:
		for _, v := range pull.Assignees {
			if _, _, e := valid(v, true); e != nil {
				return nil, false, e
			}
		}
		return nonnil(pull.Assignees), true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MILESTONE:
		if pull.Milestone == nil {
			return nil, false, nil
		}
		return valid(pull.GetMilestone(), true)
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_LABELS:
		for _, v := range pull.Labels {
			if _, _, e := valid(v, true); e != nil {
				return nil, false, e
			}
		}
		return nonnil(pull.Labels), true, nil
	case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_COMMENTS:
		if pull.CommentCount < 0 {
			return nil, false, fmt.Errorf("invalid comments")
		}
		return pull.CommentCount, true, nil
	default:
		return nil, false, fmt.Errorf("invalid pull field")
	}
}

func pullStateName(state repowolfv1.GiteaPullState) string {
	if state == repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN {
		return "open"
	}
	if state == repowolfv1.GiteaPullState_GITEA_PULL_STATE_CLOSED {
		return "closed"
	}
	return ""
}
func timeValue(value interface{ AsTime() time.Time }) string {
	return value.AsTime().UTC().Format(time.RFC3339)
}

func reviewActorFields(actor *repowolfv1.GiteaReviewActor) ([]orderedField, error) {
	if actor == nil {
		return nil, fmt.Errorf("invalid review actor")
	}
	switch value := actor.Actor.(type) {
	case *repowolfv1.GiteaReviewActor_User:
		if value.User == nil || value.User.Id <= 0 || value.User.Login == "" || !validOutputString(value.User.Login) {
			return nil, fmt.Errorf("invalid review user")
		}
		return []orderedField{{"user", json.RawMessage(fmt.Sprintf(`{"id":%d,"login":%q}`, value.User.Id, value.User.Login)), true}}, nil
	case *repowolfv1.GiteaReviewActor_Team:
		if value.Team == nil || value.Team.Id <= 0 || value.Team.Name == "" || !validOutputString(value.Team.Name) {
			return nil, fmt.Errorf("invalid review team")
		}
		return []orderedField{{"team", json.RawMessage(fmt.Sprintf(`{"id":%d,"name":%q}`, value.Team.Id, value.Team.Name)), true}}, nil
	default:
		return nil, fmt.Errorf("invalid review actor")
	}
}

func pullReviewFields(review *repowolfv1.GiteaPullReviewRecord) ([]orderedField, error) {
	if review == nil || review.Id <= 0 || review.CodeCommentCount < 0 || !validOutputString(review.Body) || !validOutputString(review.CommitId) || !validOutputString(review.Url) || !validTime(review.Submitted) {
		return nil, fmt.Errorf("invalid review")
	}
	actorFields, err := reviewActorFields(review.Actor)
	if err != nil {
		return nil, err
	}
	actorRaw, err := marshalOrderedObject(actorFields)
	if err != nil {
		return nil, err
	}
	states := map[repowolfv1.GiteaPullReviewState]string{repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_APPROVED: "approved", repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_PENDING: "pending", repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_COMMENT: "comment", repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_REQUEST_CHANGES: "request_changes", repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_REQUEST_REVIEW: "request_review"}
	state, ok := states[review.State]
	if !ok {
		return nil, fmt.Errorf("invalid review state")
	}
	return []orderedField{{"id", review.Id, true}, {"actor", json.RawMessage(actorRaw), true}, {"state", state, true}, {"commit-id", review.CommitId, true}, {"stale", review.Stale, true}, {"official", review.Official, true}, {"dismissed", review.Dismissed, true}, {"code-comment-count", review.CodeCommentCount, true}, {"submitted", timeValue(review.Submitted), true}, {"url", review.Url, true}, {"body", review.Body, true}}, nil
}
func renderedActors(values []*repowolfv1.GiteaReviewActor) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, len(values))
	for i, v := range values {
		fields, e := reviewActorFields(v)
		if e != nil {
			return nil, e
		}
		out[i], e = marshalOrderedObject(fields)
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
func renderedReviews(values []*repowolfv1.GiteaPullReviewRecord) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, len(values))
	for i, v := range values {
		fields, e := pullReviewFields(v)
		if e != nil {
			return nil, e
		}
		out[i], e = marshalOrderedObject(fields)
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
