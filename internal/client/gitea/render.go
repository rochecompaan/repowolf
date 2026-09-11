package gitea

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

const maxRenderedBytes = 8 << 20

var repositoryHeaders = []string{"full_name", "description", "default_branch", "url", "ssh_url", "clone_url", "private", "archived", "fork", "mirror", "empty", "stars", "forks", "open_issues", "size", "topics", "created", "updated"}

type repositoryJSON struct {
	FullName      string   `json:"full_name"`
	Description   string   `json:"description"`
	DefaultBranch string   `json:"default_branch"`
	URL           string   `json:"url"`
	SSHURL        string   `json:"ssh_url"`
	CloneURL      string   `json:"clone_url"`
	Private       bool     `json:"private"`
	Archived      bool     `json:"archived"`
	Fork          bool     `json:"fork"`
	Mirror        bool     `json:"mirror"`
	Empty         bool     `json:"empty"`
	Stars         uint64   `json:"stars"`
	Forks         uint64   `json:"forks"`
	OpenIssues    uint64   `json:"open_issues"`
	Size          uint64   `json:"size"`
	Topics        []string `json:"topics"`
	Created       string   `json:"created"`
	Updated       string   `json:"updated"`
}

func render(parsed command, response *repowolfv1.GiteaResponse) ([]byte, error) {
	if response == nil || response.GetMeta().GetRequestId() == "" || response.GetRepositoryView() == nil || response.GetRepositoryView().GetRepository() == nil {
		return nil, fmt.Errorf("invalid response")
	}
	repository := response.GetRepositoryView().GetRepository()
	if repository.Created == nil || repository.Updated == nil || !repository.Created.IsValid() || !repository.Updated.IsValid() {
		return nil, fmt.Errorf("invalid response timestamp")
	}
	value := repositoryJSON{
		FullName: repository.FullName, Description: repository.Description, DefaultBranch: repository.DefaultBranch,
		URL: repository.Url, SSHURL: repository.SshUrl, CloneURL: repository.CloneUrl,
		Private: repository.Private, Archived: repository.Archived, Fork: repository.Fork, Mirror: repository.Mirror, Empty: repository.Empty,
		Stars: repository.Stars, Forks: repository.Forks, OpenIssues: repository.OpenIssues, Size: repository.Size,
		Topics: append([]string{}, repository.Topics...), Created: repository.Created.AsTime().UTC().Format(time.RFC3339), Updated: repository.Updated.AsTime().UTC().Format(time.RFC3339),
	}
	var output []byte
	var err error
	switch parsed.format {
	case outputJSON:
		output, err = json.Marshal(value)
		output = append(output, '\n')
	case outputSimple, outputTable:
		cells := repositoryCells(value)
		var buffer bytes.Buffer
		if parsed.format == outputTable {
			buffer.WriteString(strings.Join(repositoryHeaders, "\t"))
			buffer.WriteByte('\n')
			buffer.WriteString(strings.Join(cells, "\t"))
			buffer.WriteByte('\n')
		} else {
			for index, header := range repositoryHeaders {
				fmt.Fprintf(&buffer, "%s: %s\n", header, cells[index])
			}
		}
		output = buffer.Bytes()
	default:
		return nil, fmt.Errorf("invalid output format")
	}
	if err != nil {
		return nil, fmt.Errorf("encode response: %w", err)
	}
	if len(output) > maxRenderedBytes {
		return nil, fmt.Errorf("rendered output exceeds client limit")
	}
	return output, nil
}

func repositoryCells(value repositoryJSON) []string {
	return []string{cell(value.FullName), cell(value.Description), cell(value.DefaultBranch), cell(value.URL), cell(value.SSHURL), cell(value.CloneURL), fmt.Sprint(value.Private), fmt.Sprint(value.Archived), fmt.Sprint(value.Fork), fmt.Sprint(value.Mirror), fmt.Sprint(value.Empty), fmt.Sprint(value.Stars), fmt.Sprint(value.Forks), fmt.Sprint(value.OpenIssues), fmt.Sprint(value.Size), cell(strings.Join(value.Topics, ", ")), value.Created, value.Updated}
}

func cell(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
}
