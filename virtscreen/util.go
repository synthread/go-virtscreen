package virtscreen

import (
	"math/rand"
	"os"
	"os/exec"
	"syscall"
	"time"

	goerr "errors"

	"github.com/pkg/errors"
)

var pwRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

// pidAlive checks if the provided pid is valid and a running process
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	errno, ok := err.(syscall.Errno)
	if !ok {
		return false
	}
	switch errno {
	case syscall.ESRCH:
		return false
	case syscall.EPERM:
		return true
	}
	return false
}

// endProcesses will kindly ask processes defined by the provided exec.Cmd
// structs to end, and if they do not after a timeout they are killed. If you
// provide an exec.Cmd that has setpgid set, the entire process group will be
// killed if the parent process does not gracefully terminate.
func endProcesses(timeout time.Duration, cmds ...*exec.Cmd) error {
	errs := []error{}

	for _, c := range cmds {
		if c == nil || c.Process == nil {
			continue
		}
		pid := c.Process.Pid
		if pid <= 0 || !pidAlive(pid) {
			continue
		}

		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			errs = append(errs, errors.Wrap(err, "failed to send SIGTERM"))
		}
	}

	// all have been sigterm'd, loop through and check for killing
	for _, c := range cmds {
		if c == nil || c.Process == nil {
			continue
		}
		pid := c.Process.Pid
		if pid <= 0 || !pidAlive(pid) {
			continue
		}

		start := time.Now()
		for {
			if !pidAlive(pid) {
				break
			}

			if time.Since(start) > timeout {
				var err error
				if c.SysProcAttr != nil && c.SysProcAttr.Setpgid {
					// terminate the entire group
					err = syscall.Kill(-pid, syscall.SIGKILL)
				} else {
					err = syscall.Kill(pid, syscall.SIGKILL)
				}

				if err != nil {
					errs = append(errs, errors.Wrap(err, "SIGKILL failed"))
				}

				break
			}

			time.Sleep(10 * time.Millisecond)
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return goerr.Join(errs...)
}

// randPassword will generate a random alphanumeric password (lowercase only) of
// the given length
func randPassword(l int) string {
	b := make([]rune, l)
	for i := range b {
		b[i] = pwRunes[rand.Intn(len(pwRunes))]
	}
	return string(b)
}
