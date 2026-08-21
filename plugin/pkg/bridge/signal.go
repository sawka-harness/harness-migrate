// Copyright © 2026 Harness Inc.
// SPDX-License-Identifier: Apache-2.0

package bridge

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// WithInterrupt returns a context cancelled on SIGINT or SIGTERM, so a long
// export writes its checkpoint on Ctrl-C instead of dying mid-write.
//
// This belongs in core — the host builds ctx.Context, and graceful cancellation
// should not be something each plugin remembers to opt into. Until it is there,
// every handler that runs a cancellable operation wraps its context here.
func WithInterrupt(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigs:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(sigs)
		cancel()
	}
}
