package github

import (
	"context"
	"net/url"
	"strconv"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

const (
	maximumCheckPages   = 10
	maximumCheckRecords = 1000
	maximumCheckBytes   = 8 * miB
)

type checkBudget struct{ raw, pages, records int }

func (adapter *Adapter) executeChecks(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	number := request.GetPullChecks().Number
	base := "/repos/" + repository.Repository.Owner + "/" + repository.Repository.Name
	first, err := adapter.call(ctx, adapter.apiCommand(repository.Provider.APIHost, "GET", base+"/pulls/"+decimal(number), nil, maximumCheckBytes))
	if err != nil {
		return nil, err
	}
	var pull struct {
		Head *apiRef `json:"head"`
	}
	if err := decode(first.Stdout, &pull); err != nil {
		return nil, err
	}
	if pull.Head == nil || pull.Head.SHA == nil || objectID(*pull.Head.SHA) != nil {
		return nil, providerResponse(nil, "pull head")
	}
	budget := checkBudget{raw: len(first.Stdout)}
	records := make([]*repowolfv1.GitHubCheckRecord, 0)
	if err := adapter.appendCheckPages(ctx, repository, base, *pull.Head.SHA, &budget, &records); err != nil {
		return nil, err
	}
	if err := adapter.appendStatusPages(ctx, repository, base, *pull.Head.SHA, &budget, &records); err != nil {
		return nil, err
	}
	response := &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullChecks{PullChecks: &repowolfv1.GitHubPullChecksResult{Checks: records}}}
	if proto.Size(response) > maximumResponseBytes {
		return nil, runner.ErrOutputLimit
	}
	return response, nil
}
func (adapter *Adapter) appendCheckPages(ctx context.Context, repository policy.ResolvedRepository, base, sha string, budget *checkBudget, records *[]*repowolfv1.GitHubCheckRecord) error {
	seen := 0
	for page := 1; ; page++ {
		raw, err := adapter.checkPage(ctx, repository, base+"/commits/"+sha+"/check-runs", page, budget)
		if err != nil {
			return err
		}
		var value struct {
			Total *int           `json:"total_count"`
			Runs  *[]apiCheckRun `json:"check_runs"`
		}
		if err := decode(raw, &value); err != nil {
			return err
		}
		if value.Total == nil || value.Runs == nil || *value.Total < 0 {
			return providerResponse(nil, "check-runs page")
		}
		if *value.Total > maximumCheckRecords {
			return runner.ErrOutputLimit
		}
		if len(*value.Runs) == 0 && seen < *value.Total {
			return providerResponse(nil, "complete check-runs")
		}
		for _, entry := range *value.Runs {
			if budget.records >= maximumCheckRecords || seen >= *value.Total {
				return runner.ErrOutputLimit
			}
			record, err := checkRecord(entry)
			if err != nil {
				return err
			}
			*records = append(*records, record)
			budget.records++
			seen++
		}
		if *value.Total <= page*100 {
			if seen < *value.Total {
				return providerResponse(nil, "complete check-runs")
			}
			return nil
		}
	}
}
func (adapter *Adapter) appendStatusPages(ctx context.Context, repository policy.ResolvedRepository, base, sha string, budget *checkBudget, records *[]*repowolfv1.GitHubCheckRecord) error {
	start := budget.records
	seen := 0
	for page := 1; ; page++ {
		raw, err := adapter.checkPage(ctx, repository, base+"/commits/"+sha+"/status", page, budget)
		if err != nil {
			return err
		}
		var value struct {
			Total    *int         `json:"total_count"`
			Statuses *[]apiStatus `json:"statuses"`
		}
		if err := decode(raw, &value); err != nil {
			return err
		}
		if value.Total == nil || value.Statuses == nil || *value.Total < 0 {
			return providerResponse(nil, "statuses page")
		}
		if *value.Total > maximumCheckRecords || *value.Total+start > maximumCheckRecords {
			return runner.ErrOutputLimit
		}
		if len(*value.Statuses) == 0 && seen < *value.Total {
			return providerResponse(nil, "complete statuses")
		}
		for _, entry := range *value.Statuses {
			if budget.records >= maximumCheckRecords || seen >= *value.Total {
				return runner.ErrOutputLimit
			}
			name, err := required(entry.Context, "status.context")
			if err != nil {
				return err
			}
			state, err := required(entry.State, "status.state")
			if err != nil {
				return err
			}
			*records = append(*records, &repowolfv1.GitHubCheckRecord{Name: name, State: state, DetailsUrl: entry.TargetURL, Description: entry.Description, StartedAt: entry.CreatedAt, CompletedAt: entry.UpdatedAt})
			budget.records++
			seen++
		}
		if *value.Total <= page*100 {
			if seen < *value.Total {
				return providerResponse(nil, "complete statuses")
			}
			return nil
		}
	}
}
func (adapter *Adapter) checkPage(ctx context.Context, repository policy.ResolvedRepository, endpoint string, page int, budget *checkBudget) ([]byte, error) {
	if budget.pages >= maximumCheckPages || budget.raw >= maximumCheckBytes {
		return nil, runner.ErrOutputLimit
	}
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"100"}}
	command := adapter.apiCommand(repository.Provider.APIHost, "GET", endpoint+"?"+query.Encode(), nil, maximumCheckBytes-budget.raw)
	result, err := adapter.call(ctx, command)
	if err != nil {
		return nil, err
	}
	budget.raw += len(result.Stdout)
	budget.pages++
	if budget.raw > maximumCheckBytes {
		return nil, runner.ErrOutputLimit
	}
	return result.Stdout, nil
}
