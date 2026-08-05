package app

import (
	"context"
	"fmt"
	"io"
	"net"
)

// Serve assembles the full runtime before binding its configured TCP address.
func Serve(ctx context.Context, configPath string, auditOutput io.Writer) error {
	runtime, err := NewRuntime(configPath, auditOutput)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", runtime.Config.Listen)
	if err != nil {
		return fmt.Errorf("bind service listener: %w", err)
	}
	return runtime.Server.Serve(ctx, listener)
}
