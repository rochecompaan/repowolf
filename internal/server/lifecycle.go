package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
)

// Serve accepts TLS gRPC traffic until ctx is cancelled. It stops admission,
// drains for the configured grace period, then cancels and force-stops work.
func (service *Server) Serve(ctx context.Context, listener net.Listener) error {
	if service == nil || listener == nil {
		return fmt.Errorf("server and listener are required")
	}
	if !service.started.CompareAndSwap(false, true) {
		return fmt.Errorf("server has already served")
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- service.grpc.Serve(listener) }()
	select {
	case err := <-serveErrors:
		service.markStopping()
		service.grpc.Stop()
		return errors.Join(err, service.finish())
	case <-ctx.Done():
		return service.shutdown()
	}
}

func (service *Server) shutdown() error {
	service.markStopping()
	drained := make(chan struct{})
	go func() {
		service.grpc.GracefulStop()
		close(drained)
	}()
	timer := time.NewTimer(service.gracePeriod)
	defer timer.Stop()
	select {
	case <-drained:
		return service.finish()
	case <-timer.C:
		incompleteAudit := service.cancelActive()
		go service.grpc.Stop()
		return service.finishForced(incompleteAudit)
	}
}

func (service *Server) markStopping() {
	if service == nil || !service.stopping.CompareAndSwap(false, true) {
		return
	}
	service.health.Shutdown()
}

func (service *Server) cancelActive() bool {
	service.activeMu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(service.active))
	for _, cancel := range service.active {
		cancellations = append(cancellations, cancel)
	}
	service.activeMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
	return len(cancellations) != 0
}

func (service *Server) finish() error {
	var cleanupErr error
	if service.cleanup != nil {
		cleanupErr = service.cleanup()
	}
	var flushErr error
	if flusher, ok := service.audit.(interface{ Flush() error }); ok {
		flushErr = flusher.Flush()
	}
	return errors.Join(cleanupErr, flushErr)
}

func (service *Server) finishForced(incompleteAudit bool) error {
	var cleanupErr error
	if service.cleanup != nil {
		cleanupErr = service.cleanup()
	}
	var flushErr error
	if flusher, ok := service.audit.(interface{ FlushIfIdle() error }); ok {
		flushErr = flusher.FlushIfIdle()
	}
	if incompleteAudit {
		return errors.Join(cleanupErr, flushErr, audit.ErrIncomplete)
	}
	return errors.Join(cleanupErr, flushErr)
}
