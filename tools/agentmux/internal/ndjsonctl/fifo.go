package ndjsonctl

import (
	"context"
	"os"
	"syscall"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

func ensureFIFO(path string) error {
	if st, err := os.Stat(path); err == nil {
		if st.Mode()&os.ModeNamedPipe != 0 {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return apperr.Wrap("ndjson_process_error", err, "replace non-fifo")
		}
	} else if !os.IsNotExist(err) {
		return apperr.Wrap("ndjson_process_error", err, "stat fifo")
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		return apperr.Wrap("ndjson_process_error", err, "create fifo")
	}
	return nil
}

func writeFIFO(ctx context.Context, path string, data []byte, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = fifoWriteTimeout
	}
	deadline := time.Now().Add(timeout)
	fd, err := openFIFOWriteNonblock(ctx, path, deadline)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	remaining := data
	for len(remaining) > 0 {
		n, err := syscall.Write(fd, remaining)
		if n > 0 {
			remaining = remaining[n:]
		}
		if len(remaining) == 0 {
			return nil
		}
		if err != nil && !isRetryableFIFOError(err) {
			return apperr.Wrap("ndjson_fifo_broken", err, "write claude input fifo")
		}
		if err := waitFIFOReady(ctx, deadline, "timed out writing claude input fifo"); err != nil {
			return err
		}
	}
	return nil
}

func openFIFOWriteNonblock(ctx context.Context, path string, deadline time.Time) (int, error) {
	for {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			return fd, nil
		}
		if err != syscall.ENXIO && err != syscall.EAGAIN && err != syscall.EINTR {
			return -1, apperr.Wrap("ndjson_fifo_broken", err, "open claude input fifo")
		}
		if err := waitFIFOReady(ctx, deadline, "timed out opening claude input fifo"); err != nil {
			return -1, err
		}
	}
}

func waitFIFOReady(ctx context.Context, deadline time.Time, timeoutMessage string) error {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return apperr.New("ndjson_fifo_broken", timeoutMessage)
	}
	delay := 25 * time.Millisecond
	if remaining < delay {
		delay = remaining
	}
	return sleepPoll(ctx, delay)
}

func isRetryableFIFOError(err error) bool {
	return err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR
}
