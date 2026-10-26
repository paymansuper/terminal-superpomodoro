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
	"strconv"
	"strings"
	"syscall"
	"time"
)

type miniTask struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type miniState struct {
	PID       int        `json:"pid"`
	Mini      bool       `json:"mini"`
	Phase     string     `json:"phase"`
	Remaining float64    `json:"remaining"`
	Total     float64    `json:"total"`
	Running   bool       `json:"running"`
	Ready     bool       `json:"ready"`
	Event     int        `json:"event"`
	Message   string     `json:"message"`
	ColorA    string     `json:"colorA"`
	ColorB    string     `json:"colorB"`
	Text      string     `json:"text"`
	Muted     string     `json:"muted"`
	Surface   string     `json:"surface"`
	Auto      bool       `json:"auto"`
	Image     string     `json:"image,omitempty"`
	ImageBr   float64    `json:"imageBrightness,omitempty"`
	ImageFit  string     `json:"imageFit,omitempty"`
	ImageSc   float64    `json:"imageScale,omitempty"`
	ImagePos  string     `json:"imagePosition,omitempty"`
	ShiftX    float64    `json:"imageShiftX,omitempty"`
	ShiftY    float64    `json:"imageShiftY,omitempty"`
	Tasks     []miniTask `json:"tasks,omitempty"`
}

func (c rgb) hexStr() string { return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b) }

func miniDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "superpomodoro")
}

// Every timer process gets its own pair of files. With shared files a mini window
// of one timer would follow another one and never close, leaving windows stacked.
func statePath() string { return filepath.Join(miniDir(), fmt.Sprintf("state-%d.json", os.Getpid())) }
func cmdPath() string   { return filepath.Join(miniDir(), fmt.Sprintf("cmd-%d", os.Getpid())) }

// cleanMiniFiles removes files left behind by timers that are no longer running,
// including the shared files of older versions.
func cleanMiniFiles() {
	os.Remove(filepath.Join(miniDir(), "state.json"))
	os.Remove(filepath.Join(miniDir(), "cmd"))
	files, _ := filepath.Glob(filepath.Join(miniDir(), "*-*"))
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".json")
		base = strings.TrimSuffix(base, ".tmp")
		i := strings.LastIndex(base, "-")
		pid, err := strconv.Atoi(base[i+1:])
		if err != nil || !(strings.HasPrefix(base, "state-") || strings.HasPrefix(base, "cmd-")) {
			continue
		}
		if pid != os.Getpid() && syscall.Kill(pid, 0) != nil {
			os.Remove(f)
		}
	}
}

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
	for _, i := range a.sessionTasks() {
		st.Tasks = append(st.Tasks, miniTask{ID: i, Text: a.todos[i].Text, Done: a.todos[i].Done})
	}
	if o := a.imageOptions(); o.Path != "" {
		st.Image, st.ImageBr, st.ImageSc, st.ImagePos = expandHome(o.Path), o.Bright, o.Scale, o.Pos
		st.ShiftX, st.ShiftY = o.ShiftX, o.ShiftY
		st.ImageFit = "cover"
		if a.img.load(o.Path) == nil {
			st.ImageFit = a.img.resolvedFit(o)
		}
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
	cleanMiniFiles()
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
	go cmd.Wait() // reap the helper when it exits, so floatAlive sees it gone
	minimizeTerminal()
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
		default:
			// task:<n> ticks a task off from the mini window
			if n, err := strconv.Atoi(strings.TrimPrefix(c, "task:")); err == nil && strings.HasPrefix(c, "task:") {
				a.toggleDone(n)
			}
		case "restore":
			a.mini = false
			restoreTerminal()
		}
	}
	a.writeMiniState()
}

// terminalApp identifies the terminal application we run in (macOS).
type terminalApp struct {
	name   string // for "tell application"
	bundle string // for System Events
	csiOK  bool   // understands the xterm minimize/restore sequences
}

func currentTerminal() (terminalApp, bool) {
	switch os.Getenv("TERM_PROGRAM") {
	case "Apple_Terminal":
		return terminalApp{"Terminal", "com.apple.Terminal", true}, true
	case "iTerm.app":
		return terminalApp{"iTerm", "com.googlecode.iterm2", true}, true
	case "ghostty":
		return terminalApp{"Ghostty", "com.mitchellh.ghostty", false}, true
	case "WezTerm":
		return terminalApp{"WezTerm", "com.github.wez.wezterm", false}, true
	case "WarpTerminal":
		return terminalApp{"Warp", "dev.warp.Warp-Stable", false}, true
	}
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "":
		return terminalApp{"kitty", "net.kovidgoyal.kitty", false}, true
	case os.Getenv("ALACRITTY_WINDOW_ID") != "" || os.Getenv("TERM") == "alacritty":
		return terminalApp{"Alacritty", "org.alacritty", false}, true
	}
	return terminalApp{}, false
}

// minimizeTerminal gets the terminal out of the way for the mini window.
// Terminal.app and iTerm2 minimize their own window on an xterm escape sequence.
// Terminals that ignore it (Ghostty, Kitty, WezTerm, ...) get hidden like Cmd+H.
func minimizeTerminal() {
	os.Stdout.WriteString("\x1b[2t")
	app, ok := currentTerminal()
	if !ok || app.csiOK || runtime.GOOS != "darwin" {
		return
	}
	spawn("osascript", "-e", fmt.Sprintf(
		`tell application "System Events" to set visible of (first application process whose bundle identifier is %q) to false`, app.bundle))
}

// restoreTerminal un-minimizes (or unhides) the terminal and brings it to the front.
func restoreTerminal() {
	os.Stdout.WriteString("\x1b[1t\x1b[5t")
	if app, ok := currentTerminal(); ok && runtime.GOOS == "darwin" {
		spawn("osascript", "-e", fmt.Sprintf(`tell application %q to activate`, app.name))
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
