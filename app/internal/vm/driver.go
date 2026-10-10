package vm

import (
	"context"
	"errors"
	"net"
)

// machine is a booted VM as seen by the helper process.
type machine interface {
	// dial opens a stream to a guest agent port (one of the vsock* constants).
	dial(ctx context.Context, port uint32) (net.Conn, error)
	// done is closed once the VM has stopped.
	done() <-chan struct{}
	// kill powers the VM off without asking the guest.
	kill() error
}

type bootOptions struct {
	manifest Manifest
	config   Config
	logf     func(string, ...any)
}

var errUnsupported = errors.New("the CARE engine is only used on macOS and Windows")
