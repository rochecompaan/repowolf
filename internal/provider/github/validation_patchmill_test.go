package github

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func TestPatchmillRequestValidation(t *testing.T) {
	issueState := repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN
	tooManyLabels := numberedLabels(101)

	validRequests := []*repowolfv1.GitHubRequest{
		{Operation: &repowolfv1.GitHubRequest_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserRequest{}}},
		{Operation: &repowolfv1.GitHubRequest_IssueList{IssueList: &repowolfv1.GitHubIssueListRequest{State: issueState, Limit: 1001}}},
		{Operation: &repowolfv1.GitHubRequest_LabelList{LabelList: &repowolfv1.GitHubLabelListRequest{Limit: 1000}}},
		{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: strings.Repeat("é", 50), Color: "1A2b3C", Description: strings.Repeat("é", 100)}}},
		{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 1, AddLabels: numberedLabels(100)}}},
	}
	for _, request := range validRequests {
		if err := ValidateGitHubRequest(request); err != nil {
			t.Fatalf("ValidateGitHubRequest(%T) = %v", request.Operation, err)
		}
	}

	invalidRequests := []struct {
		name    string
		request *repowolfv1.GitHubRequest
	}{
		{"nil current user", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_CurrentUser{}}},
		{"nil label list", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelList{}}},
		{"nil label create", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{}}},
		{"nil issue label change", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{}}},
		{"zero issue number", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{AddLabels: []string{"label"}}}}},
		{"issue limit 1002", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueList{IssueList: &repowolfv1.GitHubIssueListRequest{State: issueState, Limit: 1002}}}},
		{"label limit 1001", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelList{LabelList: &repowolfv1.GitHubLabelListRequest{Limit: 1001}}}},
		{"empty change set", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 1}}}},
		{"overlapping changes", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 1, AddLabels: []string{"label"}, RemoveLabels: []string{"label"}}}}},
		{"duplicate changes", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 1, AddLabels: []string{"label", "label"}}}}},
		{"more than 100 labels", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 1, AddLabels: tooManyLabels}}}},
		{"invalid UTF-8 label", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 1, AddLabels: []string{"\xff"}}}}},
		{"NUL label", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 1, AddLabels: []string{"label\x00"}}}}},
		{"empty label name", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Color: "123456"}}}},
		{"51 code point label name", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: strings.Repeat("é", 51), Color: "123456"}}}},
		{"NUL label name", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "label\x00", Color: "123456"}}}},
		{"control label name", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "label\n", Color: "123456"}}}},
		{"101 code point description", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "label", Color: "123456", Description: strings.Repeat("é", 101)}}}},
		{"invalid UTF-8 description", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "label", Color: "123456", Description: "\xff"}}}},
		{"NUL description", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "label", Color: "123456", Description: "description\x00"}}}},
		{"invalid color length", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "label", Color: "12345"}}}},
		{"invalid color character", &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "label", Color: "12345g"}}}},
	}
	for _, test := range invalidRequests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateGitHubRequest(test.request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("ValidateGitHubRequest() = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestPatchmillOversizedLabelChangesRejectBeforeSeenAllocation(t *testing.T) {
	labels := numberedLabels(101)
	request := func(number uint64) *repowolfv1.GitHubRequest {
		return &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: number, AddLabels: labels}}}
	}
	allocations := testing.AllocsPerRun(100, func() {
		if err := ValidateGitHubRequest(request(1)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("ValidateGitHubRequest() = %v, want ErrInvalidRequest", err)
		}
	})
	baseline := testing.AllocsPerRun(100, func() {
		if err := ValidateGitHubRequest(request(0)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("ValidateGitHubRequest() = %v, want ErrInvalidRequest", err)
		}
	})
	if allocations > baseline {
		t.Fatalf("oversized label validation allocations = %v, baseline = %v", allocations, baseline)
	}
}

func numberedLabels(count int) []string {
	labels := make([]string, count)
	for index := range labels {
		labels[index] = "label-" + strconv.Itoa(index)
	}
	return labels
}
