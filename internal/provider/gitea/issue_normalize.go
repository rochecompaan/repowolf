package gitea

import (
	sdk "code.gitea.io/sdk/gitea"
	"fmt"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
	"time"
	"unicode/utf8"
)

type normalizedIssue struct {
	index, authorID, commentCount         int64
	state                                 repowolfv1.GiteaIssueState
	author, url, title, body, owner, repo string
	created, updated                      time.Time
	deadline                              *time.Time
	assignees, labels                     []string
	milestone                             *string
}

func validProviderString(v string) bool { return utf8.ValidString(v) && !strings.ContainsRune(v, 0) }
func normalizeIssue(v *sdk.Issue, owner, repo string) (*normalizedIssue, error) {
	if v == nil || v.PullRequest != nil || v.Index <= 0 || v.Poster == nil || v.Poster.ID <= 0 || v.Comments < 0 || v.Repository == nil || v.Repository.Owner != owner || v.Repository.Name != repo {
		return nil, fmt.Errorf("invalid issue")
	}
	state := repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_UNSPECIFIED
	if v.State == sdk.StateOpen {
		state = 1
	} else if v.State == sdk.StateClosed {
		state = 2
	} else {
		return nil, fmt.Errorf("invalid state")
	}
	values := []string{v.Poster.UserName, v.HTMLURL, v.Title, v.Body, v.Repository.Owner, v.Repository.Name}
	for _, s := range values {
		if !validProviderString(s) {
			return nil, fmt.Errorf("invalid issue string")
		}
	}
	if v.Poster.UserName == "" || v.HTMLURL == "" || v.Title == "" || v.Created.IsZero() || v.Updated.IsZero() || v.Updated.Before(v.Created) {
		return nil, fmt.Errorf("invalid issue fields")
	}
	n := &normalizedIssue{index: v.Index, authorID: v.Poster.ID, commentCount: int64(v.Comments), state: state, author: v.Poster.UserName, url: v.HTMLURL, title: v.Title, body: v.Body, owner: owner, repo: repo, created: v.Created, updated: v.Updated}
	if v.Deadline != nil {
		if v.Deadline.IsZero() {
			return nil, fmt.Errorf("invalid deadline")
		}
		d := *v.Deadline
		n.deadline = &d
	}
	if v.Milestone != nil {
		if v.Milestone.Title == "" || !validProviderString(v.Milestone.Title) {
			return nil, fmt.Errorf("invalid milestone")
		}
		m := v.Milestone.Title
		n.milestone = &m
	}
	n.assignees = make([]string, len(v.Assignees))
	for i, a := range v.Assignees {
		if a == nil || a.UserName == "" || !validProviderString(a.UserName) {
			return nil, fmt.Errorf("invalid assignee")
		}
		n.assignees[i] = a.UserName
	}
	n.labels = make([]string, len(v.Labels))
	for i, l := range v.Labels {
		if l == nil || l.Name == "" || !validProviderString(l.Name) {
			return nil, fmt.Errorf("invalid label")
		}
		n.labels[i] = l.Name
	}
	return n, nil
}
func projectIssue(n *normalizedIssue, fields []repowolfv1.GiteaIssueField) *repowolfv1.GiteaIssueRecord {
	r := &repowolfv1.GiteaIssueRecord{Index: n.index, State: n.state, Kind: 1}
	for _, f := range fields {
		switch f {
		case 3:
			r.Author = n.author
		case 4:
			r.AuthorId = n.authorID
		case 5:
			r.Url = n.url
		case 6:
			r.Title = n.title
		case 7:
			r.Body = n.body
		case 8:
			r.Created = timestamppb.New(n.created)
		case 9:
			r.Updated = timestamppb.New(n.updated)
		case 10:
			if n.deadline != nil {
				r.Deadline = timestamppb.New(*n.deadline)
			}
		case 11:
			r.Assignees = append([]string{}, n.assignees...)
		case 12:
			if n.milestone != nil {
				m := *n.milestone
				r.Milestone = &m
			}
		case 13:
			r.Labels = append([]string{}, n.labels...)
		case 14:
			r.CommentCount = n.commentCount
		case 15:
			r.Repo = n.repo
		case 16:
			r.Owner = n.owner
		}
	}
	return r
}

var allIssueFields = []repowolfv1.GiteaIssueField{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}

func normalizeComment(v *sdk.Comment) (*repowolfv1.GiteaCommentRecord, error) {
	if v == nil || v.ID <= 0 || v.Poster == nil || v.Poster.ID <= 0 || v.Poster.UserName == "" || v.HTMLURL == "" || v.Created.IsZero() || v.Updated.IsZero() || v.Updated.Before(v.Created) {
		return nil, fmt.Errorf("invalid comment")
	}
	for _, s := range []string{v.Poster.UserName, v.HTMLURL, v.Body} {
		if !validProviderString(s) {
			return nil, fmt.Errorf("invalid comment string")
		}
	}
	return &repowolfv1.GiteaCommentRecord{Id: v.ID, AuthorId: v.Poster.ID, Author: v.Poster.UserName, Url: v.HTMLURL, Body: v.Body, Created: timestamppb.New(v.Created), Updated: timestamppb.New(v.Updated)}, nil
}
