// Package gitservice implements bounded, policy-authorized Git smart-protocol streams.
package gitservice

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const stderrLimitBytes = 1 << 20

var (
	trustedOwner        = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})?$`)
	trustedName         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	errTerminalDelivery = errors.New("git terminal delivery failed")
	errTerminalAudit    = errors.New("git terminal audit failed")
)

// ProcessRunner starts a pinned provider process.
type ProcessRunner interface {
	Start(context.Context, runner.Command) (*runner.Process, error)
}

// Options are immutable Git service dependencies.
type Options struct {
	Policy      *policy.Snapshot
	SSHPath     string
	Environment []string
	Limits      config.Limits
	Runner      ProcessRunner
	Audit       audit.Sink
}

// Service implements the GitService streaming RPCs.
type Service struct {
	repowolfv1.UnimplementedGitServiceServer
	options Options
}

// New constructs a bounded Git service from startup-pinned dependencies.
func New(options Options) (*Service, error) {
	if options.Policy == nil || options.Runner == nil || options.Audit == nil || options.SSHPath == "" {
		return nil, fmt.Errorf("incomplete Git service dependencies")
	}
	limits := options.Limits
	if limits.MaxStreamChunkBytes <= 0 || limits.MaxStreamChunkBytes > maxChunkBytes ||
		limits.MaxPushPrefixBytes <= 0 || limits.MaxPushPrefixBytes > 1<<20 ||
		limits.MaxGitBytesPerDirection <= 0 || limits.MaxGitBytesPerDirection > 8<<30 ||
		limits.InitialStreamTimeout <= 0 || limits.OperationTimeout <= 0 || limits.IdleStreamTimeout <= 0 {
		return nil, fmt.Errorf("invalid Git service limits")
	}
	options.Environment = append([]string{}, options.Environment...)
	return &Service{options: options}, nil
}

func (service *Service) command(ctx context.Context, open *repowolfv1.GitOpen, capability config.Capability, remoteService string) (runner.Command, policy.ResolvedRepository, error) {
	if service == nil || service.options.Policy == nil || open == nil || open.Repository == nil {
		return runner.Command{}, policy.ResolvedRepository{}, rpcstatus.ErrInvalidArgument
	}
	selector := open.Repository
	if selector.Host == "" || selector.Owner == "" || selector.Name == "" || selector.SshPort == 0 || selector.SshPort > 65535 {
		return runner.Command{}, policy.ResolvedRepository{}, rpcstatus.ErrInvalidArgument
	}
	principal, ok := auth.Principal(ctx)
	if !ok {
		return runner.Command{}, policy.ResolvedRepository{}, rpcstatus.ErrUnauthenticated
	}
	repository, err := service.options.Policy.Resolve(principal, policy.Selector{
		Kind: config.ProviderGitHub, Host: selector.Host, SSHPort: uint16(selector.SshPort), Owner: selector.Owner, Name: selector.Name,
	}, capability)
	if err != nil || repository.Provider.Kind != config.ProviderGitHub {
		return runner.Command{}, policy.ResolvedRepository{}, policy.ErrDenied
	}
	if !trustedOwner.MatchString(repository.Repository.Owner) || !trustedName.MatchString(repository.Repository.Name) {
		return runner.Command{}, policy.ResolvedRepository{}, rpcstatus.ErrInvalidArgument
	}
	if remoteService != "git-upload-pack" && remoteService != "git-receive-pack" {
		return runner.Command{}, policy.ResolvedRepository{}, rpcstatus.ErrInvalidArgument
	}
	limit := int(service.options.Limits.MaxGitBytesPerDirection)
	remote := remoteService + " '" + repository.Repository.Owner + "/" + repository.Repository.Name + ".git'"
	command := runner.Command{
		Path: service.options.SSHPath,
		Args: []string{"-T", "-p", strconv.Itoa(int(repository.Provider.SSHPort)), "--", repository.Provider.SSHUser + "@" + repository.Provider.GitHost, remote},
		Env:  append([]string{}, service.options.Environment...), Timeout: service.options.Limits.OperationTimeout,
		StdinLimit: limit, StdoutLimit: limit, StderrLimit: stderrLimitBytes,
	}
	return command, repository, nil
}

// UploadPack relays a bounded, authorized upload-pack session.
func (service *Service) UploadPack(stream repowolfv1.GitService_UploadPackServer) error {
	return service.uploadPack(stream)
}

func (service *Service) uploadPack(stream gitStream) error {
	started := time.Now()
	sender := newSendPump(stream)
	defer sender.Stop()
	state := &clientFrameState{}
	first, err := receiveWithTimeout(stream.Context(), service.options.Limits.InitialStreamTimeout, stream.Recv)
	if err == nil {
		err = state.Accept(first)
	}
	if err != nil {
		return service.finish(stream, sender, policy.ResolvedRepository{}, "git.upload-pack", started, 0, 0, nil, 0, err)
	}
	command, repository, err := service.command(stream.Context(), first.GetOpen(), config.GitRead, "git-upload-pack")
	if err != nil {
		return service.finish(stream, sender, repository, "git.upload-pack", started, 0, 0, nil, 0, err)
	}
	if err := service.writeAudit(stream.Context(), repository, "git.upload-pack", audit.OutcomeAccepted, "", started, 0, 0, nil, 0); err != nil {
		return service.finish(stream, sender, repository, "git.upload-pack", started, 0, 0, nil, 0, rpcstatus.ErrServiceUnavailable)
	}

	processContext, cancel := context.WithCancelCause(stream.Context())
	process, err := service.options.Runner.Start(processContext, command)
	if err != nil {
		cancel(err)
		return service.finish(stream, sender, repository, "git.upload-pack", started, 0, 0, nil, 0, err)
	}
	activity := make(chan struct{}, 1)
	go watchIdle(processContext, service.options.Limits.IdleStreamTimeout, activity, cancel)
	type copyResult struct {
		bytes int64
		err   error
	}
	inputDone := make(chan copyResult, 1)
	outputDone := make(chan copyResult, 1)
	go func() {
		count, copyErr := copyFramesToInput(stream, process.Stdin, state, activity)
		if copyErr != nil {
			cancel(copyErr)
		}
		inputDone <- copyResult{bytes: count, err: copyErr}
	}()
	go func() {
		count, copyErr := copyOutputToFrames(processContext, process.Stdout, sender, service.options.Limits.MaxStreamChunkBytes, activity)
		if copyErr != nil {
			cancel(copyErr)
		}
		outputDone <- copyResult{bytes: count, err: copyErr}
	}()
	var input, output copyResult
	inputPending, outputPending := true, true
	operationDone := processContext.Done()
	var operationCause error
	for inputPending && outputPending && operationDone != nil {
		select {
		case value := <-inputDone:
			input = value
			inputPending = false
			if value.err != nil {
				cancel(value.err)
			}
		case value := <-outputDone:
			output = value
			outputPending = false
			if value.err != nil {
				cancel(value.err)
			}
		case <-operationDone:
			operationCause = context.Cause(processContext)
			operationDone = nil
		}
	}
	if inputPending && operationDone != nil {
		cancel(runner.ErrCommandFailed)
	}
	if outputPending {
		output = <-outputDone
	}
	result, waitErr := process.Wait()
	if inputPending {
		input.bytes = result.StdinBytes
	}
	cause := firstError(context.Cause(processContext), operationCause, input.err, output.err, waitErr)
	if result.TimedOut {
		cause = context.DeadlineExceeded
	}
	cancel(cause)
	return service.finish(stream, sender, repository, "git.upload-pack", started, input.bytes, output.bytes, nil, 0, causeWithExit(cause, result.ExitCode))
}

func firstError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

type processFailure struct {
	err      error
	exitCode int
}

func (failure processFailure) Error() string { return "git provider operation failed" }
func (failure processFailure) Unwrap() error { return failure.err }

func causeWithExit(err error, exitCode int) error {
	if err == nil && exitCode == 0 {
		return nil
	}
	if err == nil {
		err = runner.ErrCommandFailed
	}
	return processFailure{err: err, exitCode: exitCode}
}

func (service *Service) finish(stream gitStream, sender *sendPump, repository policy.ResolvedRepository, operation string, started time.Time, inputBytes, outputBytes int64, refs []string, updates int, operationErr error) error {
	category := terminalCategory(operationErr)
	exitCode := int32(0)
	if category != repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED {
		exitCode = 1
		var failure processFailure
		if errors.As(operationErr, &failure) && failure.exitCode > 0 {
			exitCode = int32(failure.exitCode)
		}
	}
	terminal := &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Terminal{Terminal: &repowolfv1.GitTerminal{ExitCode: exitCode, Category: category}}}
	var frameState serverFrameState
	_, validationErr := frameState.Accept(terminal)
	var deliveryErr error
	if validationErr != nil {
		deliveryErr = validationErr
	} else {
		deliveryErr = sender.SendFinal(stream.Context(), service.options.Limits.IdleStreamTimeout, terminal)
	}
	outcome := audit.OutcomeCompleted
	if operationErr != nil {
		outcome = audit.OutcomeFailed
		if errors.Is(operationErr, policy.ErrDenied) || errors.Is(operationErr, policy.ErrRefPolicy) {
			outcome = audit.OutcomeDenied
		} else if errors.Is(operationErr, context.Canceled) || errors.Is(operationErr, context.DeadlineExceeded) {
			outcome = audit.OutcomeCancelled
		}
	}
	auditErr := service.writeAudit(stream.Context(), repository, operation, outcome, category.String(), started, inputBytes, outputBytes, refs, updates)
	return finishDeliveryError(deliveryErr, auditErr)
}

func finishDeliveryError(deliveryErr, auditErr error) error {
	var failures []error
	if deliveryErr != nil {
		failures = append(failures, errTerminalDelivery)
	}
	if auditErr != nil {
		failures = append(failures, errTerminalAudit)
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.Join(append([]error{rpcstatus.ErrServiceUnavailable}, failures...)...)
}

func terminalCategory(err error) repowolfv1.GitTerminalCategory {
	switch {
	case err == nil:
		return repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED
	case errors.Is(err, policy.ErrDenied):
		return repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PERMISSION_DENIED
	case errors.Is(err, errInvalidFrame), errors.Is(err, rpcstatus.ErrInvalidArgument), errors.Is(err, policy.ErrRefPolicy):
		return repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST
	case errors.Is(err, errChunkLimit), errors.Is(err, runner.ErrInputLimit), errors.Is(err, runner.ErrOutputLimit), errors.Is(err, rpcstatus.ErrResourceExhausted):
		return repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_LIMIT_EXCEEDED
	case errors.Is(err, context.DeadlineExceeded):
		return repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_DEADLINE_EXCEEDED
	case errors.Is(err, runner.ErrStartFailed), errors.Is(err, runner.ErrCommandFailed), errors.Is(err, runner.ErrCleanupFailed):
		return repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE
	default:
		return repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_UNAVAILABLE
	}
}

func (service *Service) writeAudit(ctx context.Context, repository policy.ResolvedRepository, operation string, outcome audit.Outcome, reason string, started time.Time, inputBytes, outputBytes int64, refs []string, updates int) error {
	if service == nil || service.options.Audit == nil {
		return rpcstatus.ErrServiceUnavailable
	}
	principal, _ := auth.Principal(ctx)
	requestID, _ := auth.RequestID(ctx)
	provider := ""
	if repository.ID != "" {
		provider = string(repository.Provider.Kind)
	}
	return service.options.Audit.Write(audit.Event{
		RequestID: requestID, Principal: principal, Provider: provider, Repository: repository.ID,
		Operation: operation, Outcome: outcome, Reason: reason, DurationMS: time.Since(started).Milliseconds(),
		InputBytes: inputBytes, OutputBytes: outputBytes, Refs: append([]string(nil), refs...), UpdateCount: updates,
	})
}
