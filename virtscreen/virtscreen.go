package virtscreen

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

// VirtScreen represents a single virtual screen instance
type VirtScreen struct {
	xvfbProc *exec.Cmd
	vncProc  *exec.Cmd
	xDisplay int
	conf     *Config
}

// Config is user-provided configuration for a virtual screen
type Config struct {
	VNCPassword string
	EnableVNC   bool
	Width       int
	Height      int
}

// NewVirtScreen will create and start a new virtual screen
func NewVirtScreen(conf *Config) (*VirtScreen, error) {
	if conf == nil {
		conf = &Config{
			EnableVNC: false,
			Width:     640,
			Height:    480,
		}
	}

	if conf.EnableVNC && len(strings.TrimSpace(conf.VNCPassword)) == 0 {
		conf.VNCPassword = randPassword(12)
	}

	if conf.Height <= 0 || conf.Width <= 0 {
		panic("invalid screen geometry")
	}

	vs := &VirtScreen{
		conf:     conf,
		xDisplay: -1,
	}

	fdR, fdW, err := os.Pipe()
	if err != nil {
		return nil, errors.Wrap(err, "could not create pipe for retrieving X display id")
	}
	defer fdR.Close()
	defer fdW.Close()

	args := []string{
		"-displayfd",
		"3",
		"-screen",
		"0",
		fmt.Sprintf("%dx%dx8", conf.Width, conf.Height),
		"-nocursor",
	}

	vs.xvfbProc = exec.Command("Xvfb", args...)
	vs.xvfbProc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	vs.xvfbProc.ExtraFiles = []*os.File{fdW}

	chDisplay := make(chan int, 1)

	// read from the pipe
	go func() {
		for {
			rd := bufio.NewReader(fdR)
			str, err := rd.ReadString('\n')
			if err != nil {
				chDisplay <- -1
				break
			}

			str = strings.TrimSpace(str)

			d, err := strconv.Atoi(str)
			if err == nil {
				chDisplay <- d
				break
			}
		}
	}()

	// try to start Xvfb
	if err := vs.xvfbProc.Start(); err != nil {
		defer vs.Stop()
		return nil, errors.Wrap(err, "could not start xvfb")
	}

	to := time.After(5 * time.Second)

	for {
		select {
		case vs.xDisplay = <-chDisplay:
			if vs.xDisplay < 0 {
				// it failed
				return nil, errors.New("failed to start xvfb")
			}
		case <-to:
			defer vs.Stop()
			return nil, errors.New("failed to start xvfb")
		case <-time.After(10 * time.Millisecond):
			if vs.xvfbProc.Process == nil || !pidAlive(vs.xvfbProc.Process.Pid) {
				defer vs.Stop()
				return nil, errors.New("xvfb exited on launch")
			}
		}

		// a display of zero is possible
		if vs.xDisplay >= 0 {
			break
		}
	}

	if vs.conf.EnableVNC {
		// we attempt to disable basically all methods of interacting with the
		// display since this is a view-only arrangement
		args := []string{
			"-display", fmt.Sprintf(":%d", vs.xDisplay),
			"-noremote", "-noclipboard", "-nosel",
			"-many", "-norc", "-no6", "-reopen",
			"-viewonly", "-shared", "-loop",
			"-nocmds", "-passwd", vs.conf.VNCPassword,
			"-quiet", "-nocursor",
			"-rfbport", fmt.Sprintf("%d", vs.VNCPort()),
		}
		env := append(os.Environ(), "X11VNC_REOPEN_DISPLAY=5")

		// start the vnc server
		vs.vncProc = exec.Command("x11vnc", args...)
		vs.vncProc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		vs.vncProc.Env = env

		if err := vs.vncProc.Start(); err != nil {
			return nil, errors.Wrap(err, "could not start vnc server")
		}

		// we do a quick check to make sure it didn't immediately terminate
		time.Sleep(100 * time.Millisecond)
		if vs.vncProc.Process == nil || !pidAlive(vs.vncProc.Process.Pid) {
			return nil, errors.Wrap(err, "vnc server exited on launch")
		}
	}

	return vs, nil
}

// VNCPort will return the running VNC port, or 0 if the X server is not running
func (vs *VirtScreen) VNCPort() int {
	if vs.xDisplay < 0 {
		return 0
	}
	return vs.xDisplay + 5900
}

// VNCPassword will return the password that can be used to connect to the VNC
// server, or a blank string if VNC is disabled
func (vs *VirtScreen) VNCPassword() string {
	if vs.conf.EnableVNC {
		return vs.conf.VNCPassword
	}
	return ""
}

// Alive will return whether the virtual screen session is alive (X is running,
// VNC is running if applicable, and we know the X display number)
func (vs *VirtScreen) Alive() bool {
	if vs.xDisplay < 0 {
		return false
	}

	if vs.xvfbProc == nil || vs.xvfbProc.Process == nil || !pidAlive(vs.xvfbProc.Process.Pid) {
		return false
	}

	// check the VNC server only if it is enabled
	if vs.conf.EnableVNC {
		if vs.vncProc == nil || vs.vncProc.Process == nil || !pidAlive(vs.vncProc.Process.Pid) {
			return false
		}
	}

	return true
}

// Stop will terminate the X server (and optionally VNC server)
func (vs *VirtScreen) Stop() error {
	return endProcesses(
		2*time.Second,
		vs.xvfbProc,
		vs.vncProc)
}
