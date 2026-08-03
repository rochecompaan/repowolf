package runner

import (
	"context"
	"io"
)

// Call executes a bounded unary provider command and fully reaps it before
// returning. Stderr is classified internally and then discarded.
func (runner *Runner) Call(ctx context.Context, command Command) (Result, error) {
	process, err := runner.Start(ctx, command)
	if err != nil {
		return Result{}, err
	}
	_ = process.Stdin.Close()
	stdout, readErr := io.ReadAll(process.Stdout)
	result, waitErr := process.Wait()
	result.Stdout = stdout
	if readErr != nil {
		if waitErr != nil {
			return result, waitErr
		}
		return result, ErrCommandFailed
	}
	return result, waitErr
}
