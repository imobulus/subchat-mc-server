package processutil

import (
	"os"
	"os/exec"
	"time"
)

func InterruptAndKill(cmd *exec.Cmd, timeout time.Duration) {
	endedChan := make(chan struct{})
	go func() {
		cmd.Wait()
		close(endedChan)
	}()
	cmd.Process.Signal(os.Interrupt)
	select {
	case <-endedChan:
		return
	case <-time.After(timeout):
	}
	cmd.Process.Kill()
}
