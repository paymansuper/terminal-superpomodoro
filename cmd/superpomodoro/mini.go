package main

// Mini window: a native macOS floating window (superpomodoro-float) that mirrors the
// timer while the terminal is minimized. The two processes talk through two files:
// state.json (written here, read by the window) and cmd (appended by the window).

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type miniState struct {
	PID       int     `json:"pid"`
	Mini      bool    `json:"mini"`
	Phase     string  `json:"phase"`
	Remaining float64 `json:"remaining"`
	Total     float64 `json:"total"`
	Running   bool    `json:"running"`
	Ready     bool    `json:"ready"`
	Event     int     `json:"event"`
	Message   string  `json:"message"`
	ColorA    string  `json:"colorA"`
	ColorB    string  `json:"colorB"`
	Text      string  `json:"text"`
	Muted     string  `json:"muted"`
	Surface   string  `json:"surface"`
	Auto      bool    `json:"auto"`
	Image     string  `json:"image,omitempty"`
	ImageBr   float64 `json:"imageBrightness,omitempty"`
}

func (c rgb) hexStr() string { return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b) }

func miniDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "superpomodoro")
}

func statePath() string { return filepath.Join(miniDir(), "state.json") }
func cmdPath() string   { return filepath.Join(miniDir(), "cmd") }

func (a *App) writeMiniState() {
	t := themes[a.theme] // the window has its own dark surface, so use raw theme colors
	ca, cb := t.FocusA, t.FocusB
	switch a.phase {
	case Short:
		ca, cb = t.BreakA, t.BreakB
	case Long:
		ca, cb = t.LongA, t.LongB
	}
	st := miniState{
		PID: os.Getpid(), Mini: a.mini, Phase: a.phase.Label(),
		Remaining: a.left().Seconds(), Total: a.total.Seconds(),
		Running: a.running, Ready: a.ready, Event: a.event, Message: a.eventMsg,
		ColorA: ca.hexStr(), ColorB: cb.hexStr(), Text: t.Text.hexStr(), Muted: t.Muted.hexStr(),
		Surface: lerp(t.Surface, rgb{0, 0, 0}, 0.35).hexStr(), Auto: a.cfg.Auto,
	}
	if p, br := a.imageSetting(); p != "" {
		st.Image, st.ImageBr = expandHome(p), br
	}
	data, _ := json.Marshal(st)
	os.MkdirAll(miniDir(), 0o755)
	tmp := statePath() + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, statePath())
	}
	a.lastMiniWrite = time.Now()
}

func findFloatHelper() string {
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		p := filepath.Join(filepath.Dir(exe), "superpomodoro-float")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("superpomodoro-float"); err == nil {
		return p
	}
	return ""
}

func (a *App) toggleMini() {
	if a.mini {
		a.mini = false
		a.writeMiniState() // the window sees mini=false and closes itself
		a.setFlash("mini window off")
		return
	}
	if runtime.GOOS != "darwin" {
		a.setFlash("the mini window is macOS only")
		return
	}
	helper := findFloatHelper()
	if helper == "" {
		a.setFlash("superpomodoro-float not found, see README")
		return
	}
	a.mini = true
	os.Remove(cmdPath())
	a.writeMiniState()
	cmd := exec.Command(helper, "--state", statePath(), "--cmd", cmdPath())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		a.mini = false
		a.setFlash("could not start mini window")
		return
	}
	a.floatPID = cmd.Process.Pid
	go cmd.Wait()                    // reap the helper when it exits, so floatAlive sees it gone
	os.Stdout.WriteString("\x1b[2t") // ask the terminal to minimize itself
}

// pollMiniCommands handles clicks and keys coming from the floating window.
func (a *App) pollMiniCommands() {
	work := cmdPath() + ".work"
	if os.Rename(cmdPath(), work) != nil {
		return
	}
	data, _ := os.ReadFile(work)
	os.Remove(work)
	for _, c := range strings.Fields(string(data)) {
		switch c {
		case "toggle":
			a.toggle()
		case "start":
			if !a.running {
				a.toggle()
			}
		case "skip":
			a.advance(false)
		case "restore":
			a.mini = false
			restoreTerminal()
		}
	}
	a.writeMiniState()
}

// restoreTerminal un-minimizes the terminal window and brings its app to the front.
func restoreTerminal() {
	os.Stdout.WriteString("\x1b[1t\x1b[5t")
	apps := map[string]string{
		"Apple_Terminal": "Terminal", "iTerm.app": "iTerm", "ghostty": "Ghostty",
		"WezTerm": "WezTerm", "vscode": "Visual Studio Code", "WarpTerminal": "Warp",
	}
	if app, ok := apps[os.Getenv("TERM_PROGRAM")]; ok && runtime.GOOS == "darwin" {
		spawn("osascript", "-e", fmt.Sprintf(`tell application %q to activate`, app))
	}
}

// floatAlive reports whether the mini window process is still running.
func (a *App) floatAlive() bool {
	return a.floatPID > 0 && syscall.Kill(a.floatPID, 0) == nil
}

// spawn starts a helper program without waiting for it, and reaps it afterwards.
func spawn(name string, args ...string) {
	c := exec.Command(name, args...)
	if c.Start() == nil {
		go c.Wait()
	}
}
