//go:build windows

package turborel

// A local connection on Windows: a named pipe, as `\\.\pipe\turborel`
// [CLIENT Connecting]. A pipe busy with other clients is tried again
// until the connect timeout runs out [CLIENT Timeouts]; once open, its
// reads and writes are not bounded, as a file opened this way takes no
// deadline, as turborel-client's pipe takes none.

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

const (
	// errorPipeBusy is Windows' ERROR_PIPE_BUSY: every instance of the
	// pipe is busy.
	errorPipeBusy syscall.Errno = 231
	// busyRetry is how long a busy pipe is waited for before it is tried
	// again, as turborel-client's BUSY_RETRY.
	busyRetry = 10 * time.Millisecond
	// securitySQOSPresent and securityIdentification let the pipe's
	// server learn who the client is but not act as it, as
	// turborel-client opens the pipe: Windows' SECURITY_SQOS_PRESENT and
	// SECURITY_IDENTIFICATION.
	securitySQOSPresent    = 0x00100000
	securityIdentification = 1 << 16
)

// openPipe opens the named pipe path for reading and writing, its server
// allowed only to identify the client.
func openPipe(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // openLocal gives the failure.
	}

	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_EXISTING, securitySQOSPresent|securityIdentification, 0)
	if err != nil {
		return nil, err //nolint:wrapcheck // openLocal gives the failure.
	}

	return os.NewFile(uintptr(h), path), nil
}

// openLocal opens the named pipe path, trying again while it is busy,
// within the timeout.
func openLocal(ctx context.Context, path string, within time.Duration) (stream, error) {
	deadline := deadlineOf(within)

	for {
		f, err := openPipe(path)
		if err == nil {
			return f, nil
		}

		if !errors.Is(err, errorPipeBusy) {
			return nil, ioFailed(verbConnect, err)
		}

		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil, ioFailed(verbConnect, os.ErrDeadlineExceeded)
		}

		select {
		case <-ctx.Done():
			return nil, ioFailed(verbConnect, ctx.Err())
		case <-time.After(busyRetry):
		}
	}
}
