package github

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

const (
	labelListPageSize     = 100
	maximumLabelListPages = 10
)

type includedPage struct {
	body    []byte
	hasNext bool
}

func (adapter *Adapter) executeLabelList(
	ctx context.Context,
	repository policy.ResolvedRepository,
	request *repowolfv1.GitHubRequest,
) (*repowolfv1.GitHubResponse, error) {
	limit := int(request.GetLabelList().GetLimit())
	budget := &aggregateBudget{limit: maximumPaginatedReadBytes}
	labels := make([]*repowolfv1.GitHubLabelRecord, 0, limit)

	for page := 1; page <= maximumLabelListPages; page++ {
		perPage := min(labelListPageSize, limit-len(labels))
		included, err := adapter.callLabelPage(ctx, repository, page, perPage, budget)
		if err != nil {
			return nil, err
		}
		records, err := normalizeLabelPage(included.body, perPage)
		if err != nil {
			return nil, err
		}
		labels = append(labels, records...)
		if !included.hasNext {
			return boundedLabelListResponse(labels)
		}
		if len(records) < perPage {
			return nil, providerResponse(nil, "labels pagination")
		}
		if len(labels) >= limit || page == maximumLabelListPages {
			return nil, runner.ErrOutputLimit
		}
	}
	return nil, runner.ErrOutputLimit
}

func (adapter *Adapter) callLabelPage(
	ctx context.Context,
	repository policy.ResolvedRepository,
	page, perPage int,
	budget *aggregateBudget,
) (includedPage, error) {
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}
	path := "/repos/" + repository.Repository.Owner + "/" + repository.Repository.Name + "/labels"
	endpoint := path + "?" + query.Encode()
	command := adapter.apiCommand(repository.Provider.APIHost, "GET", endpoint, nil, maximumPaginatedReadBytes)
	command.Args = append([]string{command.Args[0], "--include"}, command.Args[1:]...)
	result, err := adapter.callBudgeted(ctx, command, budget)
	if err != nil {
		return includedPage{}, err
	}
	return decodeIncludedRepositoryPage(result.Stdout, page, perPage, repository.Provider.APIHost, path)
}

func normalizeLabelPage(raw []byte, maximum int) ([]*repowolfv1.GitHubLabelRecord, error) {
	var values *[]apiLabel
	if err := decode(raw, &values); err != nil {
		return nil, err
	}
	if values == nil {
		return nil, providerResponse(nil, "labels")
	}
	if len(*values) > maximum {
		return nil, runner.ErrOutputLimit
	}
	records := make([]*repowolfv1.GitHubLabelRecord, 0, len(*values))
	for _, value := range *values {
		record, err := labelRecord(value)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func boundedLabelListResponse(labels []*repowolfv1.GitHubLabelRecord) (*repowolfv1.GitHubResponse, error) {
	response := &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_LabelList{LabelList: &repowolfv1.GitHubLabelListResult{Labels: labels}}}
	if proto.Size(response) > maximumResponseBytes {
		return nil, runner.ErrOutputLimit
	}
	return response, nil
}

type expectedLinkTarget struct {
	host    string
	path    string
	perPage int
}

func decodeIncludedPage(raw []byte, expectedPage int) (includedPage, error) {
	return decodeIncludedPageForTarget(raw, expectedPage, nil)
}

func decodeIncludedRepositoryPage(raw []byte, expectedPage, perPage int, host, path string) (includedPage, error) {
	return decodeIncludedPageForTarget(raw, expectedPage, &expectedLinkTarget{host: host, path: path, perPage: perPage})
}

func decodeIncludedPageForTarget(raw []byte, expectedPage int, target *expectedLinkTarget) (includedPage, error) {
	reader := bufio.NewReader(bytes.NewReader(raw))
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		return includedPage{}, providerResponse(err, "HTTP response")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return includedPage{}, providerResponse(nil, "successful HTTP status")
	}
	links := response.Header.Values("Link")
	if len(links) > 1 {
		return includedPage{}, providerResponse(nil, "Link header")
	}
	hasNext := false
	if len(links) == 1 {
		hasNext, err = decodeLinkHeader(links[0], expectedPage, target)
		if err != nil {
			return includedPage{}, err
		}
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return includedPage{}, providerResponse(err, "HTTP body")
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		return includedPage{}, providerResponse(err, "trailing HTTP data")
	}
	return includedPage{body: body, hasNext: hasNext}, nil
}

func decodeLinkHeader(raw string, expectedPage int, target *expectedLinkTarget) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, providerResponse(nil, "Link header")
	}
	relations := make(map[string]int)
	for _, rawLink := range strings.Split(raw, ",") {
		page, relation, err := decodePageLink(strings.TrimSpace(rawLink), target)
		if err != nil {
			return false, err
		}
		if _, duplicate := relations[relation]; duplicate {
			return false, providerResponse(nil, "Link relation")
		}
		relations[relation] = page
	}
	if err := validatePageRelations(relations, expectedPage); err != nil {
		return false, err
	}
	_, hasNext := relations["next"]
	return hasNext, nil
}

func decodePageLink(raw string, expected *expectedLinkTarget) (int, string, error) {
	closeBracket := strings.IndexByte(raw, '>')
	if !strings.HasPrefix(raw, "<") || closeBracket < 2 {
		return 0, "", providerResponse(nil, "Link target")
	}
	parsed, err := url.Parse(raw[1:closeBracket])
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return 0, "", providerResponse(err, "Link target")
	}
	parameters := strings.TrimSpace(raw[closeBracket+1:])
	const prefix = `; rel="`
	if !strings.HasPrefix(parameters, prefix) || !strings.HasSuffix(parameters, `"`) {
		return 0, "", providerResponse(nil, "Link relation")
	}
	relation := strings.TrimSuffix(strings.TrimPrefix(parameters, prefix), `"`)
	if relation != "next" && relation != "prev" && relation != "first" && relation != "last" {
		return 0, "", providerResponse(nil, "Link relation")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return 0, "", providerResponse(err, "Link query")
	}
	pages := query["page"]
	if len(pages) != 1 {
		return 0, "", providerResponse(nil, "Link page")
	}
	page, err := strconv.Atoi(pages[0])
	if err != nil || page < 1 {
		return 0, "", providerResponse(err, "Link page")
	}
	if expected != nil && !validExpectedLinkTarget(parsed, query, expected) {
		return 0, "", providerResponse(nil, "Link target")
	}
	return page, relation, nil
}

func validExpectedLinkTarget(parsed *url.URL, query url.Values, expected *expectedLinkTarget) bool {
	if !strings.EqualFold(parsed.Host, expected.host) || parsed.EscapedPath() != expected.path || len(query) != 2 {
		return false
	}
	perPage := query["per_page"]
	if len(perPage) != 1 {
		return false
	}
	value, err := strconv.Atoi(perPage[0])
	return err == nil && value == expected.perPage
}

func validatePageRelations(relations map[string]int, expectedPage int) error {
	if next, ok := relations["next"]; ok && next != expectedPage+1 {
		return providerResponse(nil, "next Link page")
	}
	if previous, ok := relations["prev"]; ok && (expectedPage == 1 || previous != expectedPage-1) {
		return providerResponse(nil, "previous Link page")
	}
	if first, ok := relations["first"]; ok && first != 1 {
		return providerResponse(nil, "first Link page")
	}
	if last, ok := relations["last"]; ok {
		_, hasNext := relations["next"]
		if hasNext && last <= expectedPage || !hasNext && last != expectedPage {
			return providerResponse(nil, "last Link page")
		}
	}
	return nil
}
