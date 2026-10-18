//go:build darwin || linux

package rex

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"

	"golang.org/x/sys/unix"
)

// pty is a pseudo-terminal and the process it runs: one the server
// started, or that the server it replaced in place started, still its
// child, as the process is the same.
type pty struct {
	master *os.File
	proc   *os.Process
}

// startPTY runs argv (argv[0] as the program sees it, path the file) in a
// new pseudo-terminal of cols×rows.
func startPTY(path string, argv []string, dir string, env []string, cols, rows int) (*pty, error) {
	master, slaveName, err := openMaster()
	if err != nil {
		return nil, err
	}
	slave, err := os.OpenFile(slaveName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("pty: opening %s: %w", slaveName, err)
	}
	defer slave.Close()
	if err := setSize(master, cols, rows); err != nil {
		master.Close()
		return nil, err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir, _ = os.UserHomeDir()
	}
	cmd := &exec.Cmd{Path: path, Args: argv, Dir: dir, Env: env, Stdin: slave, Stdout: slave, Stderr: slave}
	// A session of its own whose controlling terminal is the slave, so that
	// the terminal's signals reach the process group in the foreground.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, err
	}
	return &pty{master: master, proc: cmd.Process}, nil
}

func (p *pty) read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *pty) write(b []byte) (int, error) { return p.master.Write(b) }

// close closes the terminal's master: read ends, once it read what the
// terminal had.
func (p *pty) close() error { return p.master.Close() }

func (p *pty) resize(cols, rows int) error { return setSize(p.master, cols, rows) }

func (p *pty) pid() int {
	if p.proc == nil {
		return 0
	}
	return p.proc.Pid
}

// kill kills the process group of the session's process.
func (p *pty) kill() {
	if pid := p.pid(); pid > 0 {
		syscall.Kill(-pid, syscall.SIGKILL)
	}
}

func setSize(f *os.File, cols, rows int) error {
	ws := &unix.Winsize{Col: uint16(clamp(cols)), Row: uint16(clamp(rows))}
	return withFD(f, func(fd int) error { return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, ws) })
}

func clamp(v int) int { return max(1, min(v, 0xFFFF)) }

// foreground returns the process group in the foreground of the terminal.
func (p *pty) foreground() int {
	var pg int
	if withFD(p.master, func(fd int) (err error) {
		pg, err = unix.IoctlGetInt(fd, unix.TIOCGPGRP)
		return err
	}) != nil {
		return 0
	}
	return pg
}

// wait waits for the process and returns its exit code, 128 plus the
// signal's number for a process a signal ended.
func (p *pty) wait() int {
	st, err := p.proc.Wait()
	if err != nil {
		return -1
	}
	if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return st.ExitCode()
}

// hangup ends the session: its processes get SIGHUP, as when a terminal
// window closes.
func (p *pty) hangup() {
	if pid := p.pid(); pid > 0 {
		syscall.Kill(-pid, syscall.SIGHUP)
		if pg := p.foreground(); pg > 0 {
			syscall.Kill(-pg, syscall.SIGHUP)
		}
	}
	p.master.Close()
}

// withFD runs fn with the file's descriptor. Unlike Fd, it leaves the
// file non-blocking, so that its reads take deadlines.
func withFD(f *os.File, fn func(fd int) error) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ferr error
	if err := rc.Control(func(fd uintptr) { ferr = fn(int(fd)) }); err != nil {
		return err
	}
	return ferr
}

// shellCommand returns the program, its arguments and the shell's name
// for a session: the user's login shell when cmd is empty, and cmd as a
// login shell when login is set.
func shellCommand(cmd []string, login bool) (path string, argv []string, name string, err error) {
	if len(cmd) == 0 {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = defaultShell
		}
		// A login shell, as terminal apps start it: "-zsh".
		return sh, []string{"-" + filepath.Base(sh)}, filepath.Base(sh), nil
	}
	path, err = exec.LookPath(cmd[0])
	if err != nil {
		return "", nil, "", err
	}
	argv = slices.Clone(cmd)
	if login {
		argv[0] = "-" + filepath.Base(cmd[0])
	}
	return path, argv, filepath.Base(cmd[0]), nil
}
