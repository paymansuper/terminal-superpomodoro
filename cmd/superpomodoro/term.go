//go:build darwin || linux || freebsd

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type winsize struct{ Row, Col, X, Y uint16 }

func termSize() (int, int) {
	ws := &winsize{}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(ws)))
	if errno != 0 || ws.Col == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// enterRaw switches the terminal to cbreak mode without echo and returns a restore func.
func enterRaw() (func(), error) {
	saved, err := stty("-g")
	if err != nil {
		return nil, err
	}
	if _, err := stty("-icanon", "-echo", "min", "1", "time", "0"); err != nil {
		return nil, err
	}
	termBg, termBgKnown = queryBackground()
	os.Stdout.WriteString("\x1b[?1049h\x1b[?25l")
	return func() {
		os.Stdout.WriteString("\x1b[0m\x1b[?25h\x1b[?1049l")
		stty(saved)
	}, nil
}

var (
	termBg      rgb
	termBgKnown bool
)

// queryBackground asks the terminal for its background color (OSC 11).
// Terminals that don't answer simply time out after 250 ms.
func queryBackground() (rgb, bool) {
	if _, err := stty("min", "0", "time", "1"); err != nil {
		return rgb{}, false
	}
	defer stty("min", "1", "time", "0")
	os.Stdout.WriteString("\x1b]11;?\x1b\\")
	var resp []byte
	buf := make([]byte, 64)
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, _ := os.Stdin.Read(buf)
		resp = append(resp, buf[:n]...)
		if i := strings.Index(string(resp), "rgb:"); i >= 0 && (strings.ContainsRune(string(resp[i:]), 7) || strings.Contains(string(resp[i:]), "\x1b")) {
			break
		}
	}
	return parseOSCColor(string(resp))
}

func parseOSCColor(s string) (rgb, bool) {
	i := strings.Index(s, "rgb:")
	if i < 0 {
		return rgb{}, false
	}
	s = strings.TrimFunc(s[i+4:], func(r rune) bool { return r == 7 || r == 0x1b || r == '\\' })
	if j := strings.IndexAny(s, "\x07\x1b"); j >= 0 {
		s = s[:j]
	}
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return rgb{}, false
	}
	var v [3]int
	for k, p := range parts {
		if len(p) < 2 {
			return rgb{}, false
		}
		n, err := strconv.ParseUint(p[:2], 16, 8)
		if err != nil {
			return rgb{}, false
		}
		v[k] = int(n)
	}
	return rgb{v[0], v[1], v[2]}, true
}
