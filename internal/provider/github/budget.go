package github

import (
	"context"

	"github.com/rochecompaan/repowolf/internal/runner"
)

const maximumPaginatedReadBytes = 8 * miB

type aggregateBudget struct {
	limit int
	used  int
}

func (budget *aggregateBudget) remaining() (int, error) {
	if budget == nil || budget.used >= budget.limit {
		return 0, runner.ErrOutputLimit
	}
	return budget.limit - budget.used, nil
}

func (budget *aggregateBudget) consume(raw []byte) error {
	if budget == nil || len(raw) > budget.limit-budget.used {
		return runner.ErrOutputLimit
	}
	budget.used += len(raw)
	return nil
}

func (adapter *Adapter) callBudgeted(ctx context.Context, command runner.Command, budget *aggregateBudget) (runner.Result, error) {
	if err := ctx.Err(); err != nil {
		return runner.Result{}, err
	}
	remaining, err := budget.remaining()
	if err != nil {
		return runner.Result{}, err
	}
	command.StdoutLimit = remaining
	result, err := adapter.call(ctx, command)
	if err != nil {
		return result, err
	}
	if err := budget.consume(result.Stdout); err != nil {
		return result, err
	}
	return result, nil
}
