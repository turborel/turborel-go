//go:build !windows

package turborel

// A local connection on a platform other than Windows: a Unix socket
// [CLIENT Connecting].

import (
	"context"
	"net"
	"time"
)

// openLocal connects to the Unix socket path within the timeout.
func openLocal(ctx context.Context, path string, within time.Duration) (stream, error) {
	dialer := net.Dialer{Timeout: within}

	c, err := dialer.DialContext(ctx, networkUnix, path)
	if err != nil {
		return nil, ioFailed(verbConnect, err)
	}

	return c, nil
}
