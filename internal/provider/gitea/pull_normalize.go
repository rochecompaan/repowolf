package gitea

import (
	"fmt"
	"time"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type normalizedPull struct {
	index, authorID, commentCount                    int64
	state                                            repowolfv1.GiteaPullState
	author, url, title, body, base, baseCommit, head string
	draft                                            bool
	mergeable, allowMaintainerEdit                   *bool
	created, updated                                 time.Time
	deadline                                         *time.Time
	assignees, labels                                []string
	milestone                                        *string
	requestedReviewers                               []*repowolfv1.GiteaReviewActor
}

func normalizePull(value *sdk.PullRequest, presence pullPresence, owner, repo string) (*normalizedPull, error) {
	if value == nil || value.Index <= 0 || value.Poster == nil || value.Poster.ID <= 0 || value.Comments < 0 || value.Base == nil || value.Head == nil || value.Base.Repository == nil || value.Head.Repository == nil || value.Base.Repository.Owner == nil || value.Head.Repository.Owner == nil || value.Base.Repository.Owner.UserName != owner || value.Base.Repository.Name != repo || value.Base.Repository.FullName != owner+"/"+repo {
		return nil, fmt.Errorf("invalid pull")
	}
	state := repowolfv1.GiteaPullState_GITEA_PULL_STATE_UNSPECIFIED
	if value.State == sdk.StateOpen {
		state = repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN
	} else if value.State == sdk.StateClosed {
		state = repowolfv1.GiteaPullState_GITEA_PULL_STATE_CLOSED
	} else {
		return nil, fmt.Errorf("invalid pull state")
	}
	if presence.mergeable == nil && value.Mergeable || presence.mergeable != nil && *presence.mergeable != value.Mergeable || presence.allowMaintainerEdit == nil && value.AllowMaintainerEdit || presence.allowMaintainerEdit != nil && *presence.allowMaintainerEdit != value.AllowMaintainerEdit {
		return nil, fmt.Errorf("pull presence mismatch")
	}
	strings := []string{value.Poster.UserName, value.HTMLURL, value.Title, value.Body, value.Base.Name, value.Base.Sha, value.Head.Name, value.Base.Repository.Owner.UserName, value.Base.Repository.Name, value.Base.Repository.FullName, value.Head.Repository.Owner.UserName, value.Head.Repository.Name, value.Head.Repository.FullName}
	for _, text := range strings {
		if !validProviderString(text) {
			return nil, fmt.Errorf("invalid pull string")
		}
	}
	if value.Poster.UserName == "" || value.HTMLURL == "" || value.Title == "" || value.Base.Name == "" || value.Base.Sha == "" || value.Head.Name == "" || value.Head.Repository.Owner == nil || value.Head.Repository.Owner.UserName == "" || value.Head.Repository.Name == "" || value.Head.Repository.FullName != "" && value.Head.Repository.FullName != value.Head.Repository.Owner.UserName+"/"+value.Head.Repository.Name || value.Created == nil || value.Updated == nil || !validProtoTime(*value.Created) || !validProtoTime(*value.Updated) || value.Updated.Before(*value.Created) {
		return nil, fmt.Errorf("invalid pull fields")
	}
	n := &normalizedPull{index: value.Index, authorID: value.Poster.ID, commentCount: int64(value.Comments), state: state, author: value.Poster.UserName, url: value.HTMLURL, title: value.Title, body: value.Body, draft: value.Draft, mergeable: copyBool(presence.mergeable), allowMaintainerEdit: copyBool(presence.allowMaintainerEdit), base: value.Base.Name, baseCommit: value.Base.Sha, head: value.Head.Name, created: *value.Created, updated: *value.Updated}
	if value.Deadline != nil {
		if !validProtoTime(*value.Deadline) {
			return nil, fmt.Errorf("invalid deadline")
		}
		deadline := *value.Deadline
		n.deadline = &deadline
	}
	if value.Milestone != nil {
		if value.Milestone.Title == "" || !validProviderString(value.Milestone.Title) {
			return nil, fmt.Errorf("invalid milestone")
		}
		m := value.Milestone.Title
		n.milestone = &m
	}
	seenIDs := map[int64]bool{}
	seenNames := map[string]bool{}
	n.assignees = make([]string, len(value.Assignees))
	for i, user := range value.Assignees {
		if user == nil || user.ID <= 0 || user.UserName == "" || !validProviderString(user.UserName) || seenIDs[user.ID] || seenNames[user.UserName] {
			return nil, fmt.Errorf("invalid assignee")
		}
		seenIDs[user.ID] = true
		seenNames[user.UserName] = true
		n.assignees[i] = user.UserName
	}
	seenIDs = map[int64]bool{}
	seenNames = map[string]bool{}
	n.labels = make([]string, len(value.Labels))
	for i, label := range value.Labels {
		if label == nil || label.ID <= 0 || label.Name == "" || !validProviderString(label.Name) || seenIDs[label.ID] || seenNames[label.Name] {
			return nil, fmt.Errorf("invalid label")
		}
		seenIDs[label.ID] = true
		seenNames[label.Name] = true
		n.labels[i] = label.Name
	}
	actorKeys := map[string]bool{}
	for _, user := range value.RequestedReviewers {
		actor, err := normalizeReviewActor(user, nil)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("u:%d", user.ID)
		if actorKeys[key] {
			return nil, fmt.Errorf("duplicate reviewer")
		}
		actorKeys[key] = true
		n.requestedReviewers = append(n.requestedReviewers, actor)
	}
	for _, team := range value.RequestedReviewersTeams {
		actor, err := normalizeReviewActor(nil, team)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("t:%d", team.ID)
		if actorKeys[key] {
			return nil, fmt.Errorf("duplicate reviewer")
		}
		actorKeys[key] = true
		n.requestedReviewers = append(n.requestedReviewers, actor)
	}
	if n.requestedReviewers == nil {
		n.requestedReviewers = []*repowolfv1.GiteaReviewActor{}
	}
	return n, nil
}
func copyBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func normalizeReviewActor(user *sdk.User, team *sdk.Team) (*repowolfv1.GiteaReviewActor, error) {
	if (user == nil) == (team == nil) {
		return nil, fmt.Errorf("invalid review actor")
	}
	if user != nil {
		if user.ID <= 0 || user.UserName == "" || !validProviderString(user.UserName) {
			return nil, fmt.Errorf("invalid review user")
		}
		return &repowolfv1.GiteaReviewActor{Actor: &repowolfv1.GiteaReviewActor_User{User: &repowolfv1.GiteaReviewUser{Id: user.ID, Login: user.UserName}}}, nil
	}
	if team.ID <= 0 || team.Name == "" || !validProviderString(team.Name) {
		return nil, fmt.Errorf("invalid review team")
	}
	return &repowolfv1.GiteaReviewActor{Actor: &repowolfv1.GiteaReviewActor_Team{Team: &repowolfv1.GiteaReviewTeam{Id: team.ID, Name: team.Name}}}, nil
}

func projectPull(n *normalizedPull, fields []repowolfv1.GiteaPullField) *repowolfv1.GiteaPullRecord {
	record := &repowolfv1.GiteaPullRecord{Index: n.index, State: n.state}
	for _, field := range fields {
		switch field {
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_AUTHOR:
			record.Author = n.author
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_AUTHOR_ID:
			record.AuthorId = n.authorID
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_URL:
			record.Url = n.url
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_TITLE:
			record.Title = n.title
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BODY:
			record.Body = n.body
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MERGEABLE:
			record.Mergeable = copyBool(n.mergeable)
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BASE:
			record.Base = n.base
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_BASE_COMMIT:
			record.BaseCommit = n.baseCommit
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_HEAD:
			record.Head = n.head
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_CREATED:
			record.Created = timestamppb.New(n.created)
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_UPDATED:
			record.Updated = timestamppb.New(n.updated)
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_DEADLINE:
			if n.deadline != nil {
				record.Deadline = timestamppb.New(*n.deadline)
			}
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_ASSIGNEES:
			record.Assignees = append([]string{}, n.assignees...)
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MILESTONE:
			if n.milestone != nil {
				m := *n.milestone
				record.Milestone = &m
			}
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_LABELS:
			record.Labels = append([]string{}, n.labels...)
		case repowolfv1.GiteaPullField_GITEA_PULL_FIELD_COMMENTS:
			record.CommentCount = n.commentCount
		}
	}
	return record
}
func projectPullDetail(n *normalizedPull) *repowolfv1.GiteaPullRecord {
	fields := make([]repowolfv1.GiteaPullField, 0, 18)
	for field := 1; field <= 18; field++ {
		fields = append(fields, repowolfv1.GiteaPullField(field))
	}
	record := projectPull(n, fields)
	record.Draft = n.draft
	record.AllowMaintainerEdit = copyBool(n.allowMaintainerEdit)
	record.RequestedReviewers = append([]*repowolfv1.GiteaReviewActor{}, n.requestedReviewers...)
	record.Reviews = []*repowolfv1.GiteaPullReviewRecord{}
	record.Comments = []*repowolfv1.GiteaCommentRecord{}
	return record
}
