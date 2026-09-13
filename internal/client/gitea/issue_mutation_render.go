package gitea

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

type issueMutationJSON struct {
	Index int64  `json:"index"`
	Title string `json:"title"`
	State string `json:"state"`
	URL   string `json:"url"`
}
type commentMutationJSON struct {
	ID   int64  `json:"id"`
	URL  string `json:"url"`
	Body string `json:"body"`
}

func renderIssueMutation(format outputFormat, issue *repowolfv1.GiteaIssueRecord) ([]byte, error) {
	if err := validateIssueRecord(issue, false); err != nil {
		return nil, err
	}
	state := "open"
	if issue.State == repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_CLOSED {
		state = "closed"
	}
	value := issueMutationJSON{issue.Index, issue.Title, state, issue.Url}
	if format == outputJSON {
		out, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return append(out, '\n'), nil
	}
	cells := []string{fmt.Sprint(value.Index), cell(value.Title), value.State, cell(value.URL)}
	if format == outputTable {
		return []byte("index\ttitle\tstate\turl\n" + strings.Join(cells, "\t") + "\n"), nil
	}
	if format != outputSimple {
		return nil, fmt.Errorf("invalid output")
	}
	return []byte(fmt.Sprintf("index: %d\ntitle: %s\nstate: %s\nurl: %s\n", value.Index, cell(value.Title), value.State, cell(value.URL))), nil
}

func renderCommentMutation(format outputFormat, comment *repowolfv1.GiteaCommentRecord) ([]byte, error) {
	if _, err := commentFields(comment); err != nil {
		return nil, err
	}
	value := commentMutationJSON{comment.Id, comment.Url, comment.Body}
	if format == outputJSON {
		out, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return append(out, '\n'), nil
	}
	if format == outputTable {
		return []byte("id\turl\tbody\n" + fmt.Sprint(value.ID) + "\t" + cell(value.URL) + "\t" + cell(value.Body) + "\n"), nil
	}
	if format != outputSimple {
		return nil, fmt.Errorf("invalid output")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "id: %d\nurl: %s\nbody: %s\n", value.ID, cell(value.URL), value.Body)
	return b.Bytes(), nil
}
