package server

import (
	"io"
	"time"
)

// copyIO wraps io.Copy for the download handler.
func copyIO(dst io.Writer, src io.Reader) (int64, error) {
	return io.Copy(dst, src)
}

// sleepForProcessKill provides a small delay for process cleanup.
func sleepForProcessKill() {
	time.Sleep(500 * time.Millisecond)
}
