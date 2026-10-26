package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

var version = "dev"

type Phase int

const (
	Focus Phase = iota
	Short
	Long
)

func (p Phase) Label() string {
	return [...]string{"FOCUS", "SHORT BREAK", "LONG BREAK"}[p]
}

type Config struct {
	Work, Short, Long time.Duration
	Rounds            int
	Theme, Font, Bar  string
	Auto, Bell        bool
	Notify, Zen       bool
	Keys              bool // show shortcut legend
	RaiseOnEnd        bool // bring the terminal to the front when a phase ends
	Sound             bool
	SoundFocus        string // played when a focus phase ends
	SoundBreak        string // played when a break ends
	Bg                string
	TermBg            string // optional override, e.g. "#1e1e2e"
	Image             string
	ImageMode         string // auto, sharp or pixel
	ImageFit          string // auto, cover or contain
	ImageScale        float64
	ImagePosition     string
	ImageShiftX       float64
	ImageShiftY       float64
	HiddenThemes      string // built-in themes the user removed, comma separated
	ImageBrightness   float64
	StatsDate         string
	StatsSessions     int
	StatsMinutes      int
}

type App struct {
	mode          uiMode
	picker        *picker
	todos         []todo
	todoSel       int
	todoStamp     string
	todoCheck     time.Time
	input         []rune
	inputEdit     int
	sharp         sharpLayer
	floatPID      int
	img           imageLayer
	imgErr        string
	imageOff      bool
	imageFlag     string
	themeCheck    time.Time
	themeStamp    string
	thImg         bool
	mini          bool
	event         int
	eventMsg      string
	lastMiniWrite time.Time
	cfg           Config
	phase         Phase
	round         int // 1-based focus round within the cycle
	total         time.Duration
	remaining     time.Duration
	deadline      time.Time
	running       bool
	ready         bool // phase finished, waiting for user to start next
	theme         int
	font          int
	bar           int
	flash         string
	bgEff         int
	thCache       *Theme
	thFor         int
	bg            background
	lastFrame     time.Time
	flashTill     time.Time
	start         time.Time
}

// ---------- config ----------

func configPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "superpomodoro", "config")
}

func defaultConfig() Config {
	return Config{
		Work: 25 * time.Minute, Short: 5 * time.Minute, Long: 15 * time.Minute,
		Rounds: 4, Theme: "tokyo-night", Font: "block", Bar: "smooth",
		Notify: true, Keys: true, RaiseOnEnd: true, Sound: true, SoundFocus: "Glass", SoundBreak: "Hero", Bg: "auto",
	}
}

func loadConfig() Config {
	c := defaultConfig()
	f, err := os.Open(configPath())
	if err != nil {
		return c
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"`)
		n, _ := strconv.Atoi(v)
		b := v == "true"
		switch k {
		case "work":
			c.Work = time.Duration(n) * time.Minute
		case "short":
			c.Short = time.Duration(n) * time.Minute
		case "long":
			c.Long = time.Duration(n) * time.Minute
		case "rounds":
			c.Rounds = n
		case "theme":
			c.Theme = v
		case "font":
			c.Font = v
		case "bar":
			c.Bar = v
		case "auto":
			c.Auto = b
		case "bell":
			c.Bell = b
		case "notify":
			c.Notify = b
		case "zen":
			c.Zen = b
		case "keys":
			c.Keys = b
		case "raise":
			c.RaiseOnEnd = b
		case "sound":
			c.Sound = b
		case "sound_focus":
			c.SoundFocus = v
		case "sound_break":
			c.SoundBreak = v
		case "bg":
			c.Bg = v
		case "term_bg":
			c.TermBg = v
		case "image":
			c.Image = v
		case "image_mode":
			c.ImageMode = v
		case "image_fit":
			c.ImageFit = v
		case "image_scale":
			c.ImageScale, _ = strconv.ParseFloat(v, 64)
		case "image_position":
			c.ImagePosition = v
		case "image_shift_x":
			c.ImageShiftX, _ = strconv.ParseFloat(v, 64)
		case "image_shift_y":
			c.ImageShiftY, _ = strconv.ParseFloat(v, 64)
		case "hidden_themes":
			c.HiddenThemes = v
		case "image_brightness":
			c.ImageBrightness, _ = strconv.ParseFloat(v, 64)
		case "stats_date":
			c.StatsDate = v
		case "stats_sessions":
			c.StatsSessions = n
		case "stats_minutes":
			c.StatsMinutes = n
		}
	}
	return c
}

func saveConfig(c Config) {
	p := configPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	var sb strings.Builder
	sb.WriteString("# SuperPomodoro config, edited automatically on quit\n")
	fmt.Fprintf(&sb, "work = %d\nshort = %d\nlong = %d\nrounds = %d\n",
		int(c.Work.Minutes()), int(c.Short.Minutes()), int(c.Long.Minutes()), c.Rounds)
	fmt.Fprintf(&sb, "theme = %q\nfont = %q\nbar = %q\n", c.Theme, c.Font, c.Bar)
	fmt.Fprintf(&sb, "auto = %t\nbell = %t\nnotify = %t\nzen = %t\nkeys = %t\nraise = %t\n", c.Auto, c.Bell, c.Notify, c.Zen, c.Keys, c.RaiseOnEnd)
	fmt.Fprintf(&sb, "sound = %t\nsound_focus = %q\nsound_break = %q\nbg = %q\nterm_bg = %q\n", c.Sound, c.SoundFocus, c.SoundBreak, c.Bg, c.TermBg)
	fmt.Fprintf(&sb, "image = %q\nimage_brightness = %g\nimage_mode = %q\nimage_fit = %q\nimage_scale = %g\nimage_position = %q\nimage_shift_x = %g\nimage_shift_y = %g\n", c.Image, c.ImageBrightness, c.ImageMode, c.ImageFit, c.ImageScale, c.ImagePosition, c.ImageShiftX, c.ImageShiftY)
	fmt.Fprintf(&sb, "hidden_themes = %q\n", c.HiddenThemes)
	fmt.Fprintf(&sb, "stats_date = %q\nstats_sessions = %d\nstats_minutes = %d\n",
		c.StatsDate, c.StatsSessions, c.StatsMinutes)
	os.WriteFile(p, []byte(sb.String()), 0o644)
}

// ---------- timer logic ----------

func (a *App) durationFor(p Phase) time.Duration {
	switch p {
	case Short:
		return a.cfg.Short
	case Long:
		return a.cfg.Long
	}
	return a.cfg.Work
}

func (a *App) setPhase(p Phase) {
	a.phase = p
	a.total = a.durationFor(p)
	a.remaining = a.total
	a.running = false
}

func (a *App) left() time.Duration {
	if a.running {
		return time.Until(a.deadline)
	}
	return a.remaining
}

func (a *App) toggle() {
	if a.running {
		a.remaining = time.Until(a.deadline)
		a.running = false
		return
	}
	a.ready = false
	a.deadline = time.Now().Add(a.remaining)
	a.running = true
}

func (a *App) adjust(d time.Duration) {
	l := a.left() + d
	if l < time.Minute && d < 0 {
		return
	}
	a.total += d
	if a.running {
		a.deadline = a.deadline.Add(d)
	} else {
		a.remaining = l
	}
}

func (a *App) advance(completed bool) {
	if a.phase == Focus {
		if completed {
			today := time.Now().Format("2006-01-02")
			if a.cfg.StatsDate != today {
				a.cfg.StatsDate, a.cfg.StatsSessions, a.cfg.StatsMinutes = today, 0, 0
			}
			a.cfg.StatsSessions++
			a.cfg.StatsMinutes += int(a.total.Minutes())
		}
		if a.round >= a.cfg.Rounds {
			a.setPhase(Long)
		} else {
			a.setPhase(Short)
		}
	} else {
		if a.phase == Long {
			a.round = 1
		} else {
			a.round++
		}
		a.setPhase(Focus)
	}
	if completed && a.cfg.Auto {
		a.toggle()
	} else {
		a.ready = completed
	}
}

func (a *App) finish() {
	prev := a.phase
	a.advance(true)
	msg := map[Phase]string{
		Focus: "Focus done. Time for a break.",
		Short: "Break over. Back to focus.",
		Long:  "Long break over. New cycle.",
	}[prev]
	if prev == Focus {
		a.endSession()
		a.playSound(a.cfg.SoundFocus)
	} else {
		a.playSound(a.cfg.SoundBreak)
	}
	a.event++
	a.eventMsg = msg
	a.notify(msg)
	// the mini window handles its own pop-up; otherwise bring the terminal forward
	if a.mini && !a.floatAlive() {
		a.mini = false // the window was closed or crashed
	}
	if !a.mini && a.cfg.RaiseOnEnd {
		restoreTerminal()
	}
}

func (a *App) notify(msg string) {
	if a.cfg.Bell {
		os.Stdout.WriteString("\a")
	}
	a.setFlash(msg)
	if !a.cfg.Notify {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title "SuperPomodoro"`, msg)
		spawn("osascript", "-e", script)
	case "linux":
		if p, err := exec.LookPath("notify-send"); err == nil {
			spawn(p, "SuperPomodoro", msg)
		}
	}
}

// playSound plays a system sound by name (macOS: /System/Library/Sounds) or a file path.
func (a *App) playSound(name string) {
	if !a.cfg.Sound || name == "" {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		path := name
		if !strings.Contains(name, "/") {
			path = "/System/Library/Sounds/" + name + ".aiff"
		}
		spawn("afplay", path)
	default:
		path := name
		if !strings.Contains(name, "/") {
			path = "/usr/share/sounds/freedesktop/stereo/complete.oga"
		}
		for _, player := range []string{"paplay", "pw-play", "aplay"} {
			if p, err := exec.LookPath(player); err == nil {
				spawn(p, path)
				return
			}
		}
		os.Stdout.WriteString("\a")
	}
}

func (a *App) setFlash(s string) {
	a.flash = s
	a.flashTill = time.Now().Add(3 * time.Second)
}

// ---------- rendering ----------

const reset = "\x1b[0m"
const bold = "\x1b[1m"

type line struct {
	s string // with escape codes
	w int    // visible width
}

func plain(c rgb, s string) line { return line{c.fg() + s + reset, utf8.RuneCountInString(s)} }

func (a *App) colors() (rgb, rgb) {
	t := a.th()
	switch a.phase {
	case Short:
		return t.BreakA, t.BreakB
	case Long:
		return t.LongA, t.LongB
	}
	return t.FocusA, t.FocusB
}

func fmtClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(math.Ceil(d.Seconds()))
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func (a *App) bigClock(text string, scale int, now time.Time) []line {
	t := a.th()
	f := fonts[a.font]
	ca, cb := a.colors()
	paused := !a.running && !a.ready && a.remaining != a.total
	blinkOff := paused && now.UnixMilli()/600%2 == 1

	// total width in cells
	cells := 0
	for i, r := range text {
		if i > 0 {
			cells++ // gap
		}
		cells += len(glyphs[r][0])
	}
	cells *= scale
	shimmer := 0.0
	if a.running {
		shimmer = 0.12 * math.Sin(float64(now.UnixMilli())/900.0)
	}
	rows := 5 * scale
	out := make([]line, 0, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		x := 0
		for i, r := range text {
			g := glyphs[r]
			if i > 0 {
				for k := 0; k < scale; k++ {
					sb.WriteString("  ")
					x++
				}
			}
			for _, ch := range g[y/scale] {
				for k := 0; k < scale; k++ {
					if ch == '#' {
						tt := float64(x)/float64(cells)*0.8 + float64(y)/float64(rows)*0.2 + shimmer
						c := lerp(ca, cb, tt)
						if blinkOff {
							c = t.Muted
						}
						sb.WriteString(c.fg() + f.Ink)
					} else {
						sb.WriteString(t.Surface.fg() + f.Fill)
					}
					x++
				}
			}
		}
		sb.WriteString(reset)
		out = append(out, line{sb.String(), cells * 2})
	}
	return out
}

func (a *App) progressBar(width int) line {
	t := a.th()
	b := bars[a.bar]
	ca, cb := a.colors()
	frac := 1 - float64(a.left())/float64(a.total)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	exact := frac * float64(width)
	full := int(exact)
	var sb strings.Builder
	for i := 0; i < width; i++ {
		switch {
		case i < full:
			sb.WriteString(lerp(ca, cb, float64(i)/float64(width)).fg() + b.Full)
		case i == full && b.Partials != nil && exact-float64(full) > 0:
			idx := int((exact - float64(full)) * float64(len(b.Partials)))
			if idx >= len(b.Partials) {
				idx = len(b.Partials) - 1
			}
			sb.WriteString(lerp(ca, cb, float64(i)/float64(width)).fg() + b.Partials[idx])
		default:
			sb.WriteString(t.Surface.fg() + b.Empty)
		}
	}
	pct := fmt.Sprintf("  %3d%%", int(frac*100))
	sb.WriteString(t.Muted.fg() + pct + reset)
	return line{sb.String(), width + utf8.RuneCountInString(pct)}
}

func (a *App) frame() string {
	w, h := termSize()
	now := time.Now()
	t := a.th()
	ca, _ := a.colors()
	var lines []line

	// header
	status := ""
	switch {
	case a.ready:
		status = "  ·  press space to start"
	case a.running:
		status = ""
	case a.remaining == a.total:
		status = "  ·  ready"
	default:
		status = "  ·  paused"
	}
	header := line{bold + ca.fg() + "● " + a.phase.Label() + reset + t.Muted.fg() + status + reset,
		2 + len(a.phase.Label()) + utf8.RuneCountInString(status)}

	clock := fmtClock(a.left())
	scale := 1
	clockW := 0
	for i, r := range clock {
		if i > 0 {
			clockW++
		}
		clockW += len(glyphs[r][0])
	}
	if clockW*4+4 <= w && h >= 26 {
		scale = 2
	}

	if !a.cfg.Zen {
		lines = append(lines, header)
		if a.mode != modeTodo && a.mode != modeInput {
			lines = append(lines, a.sessionLines(w)...)
		}
		lines = append(lines, line{})
	}
	clockFrom := len(lines)
	if w < clockW*2+2 || h < 9 {
		lines = append(lines, line{bold + ca.fg() + clock + reset, len(clock)})
	} else {
		lines = append(lines, a.bigClock(clock, scale, now)...)
	}
	clockTo := len(lines)
	lines = append(lines, line{})
	barW := clockW * 2 * scale
	if barW > w-10 {
		barW = w - 10
	}
	if barW < 10 {
		barW = 10
	}
	lines = append(lines, a.progressBar(barW))

	if !a.cfg.Zen {
		lines = append(lines, line{})
		// rounds
		var rb strings.Builder
		for i := 1; i <= a.cfg.Rounds; i++ {
			if i < a.round || (i == a.round && a.phase != Focus) {
				rb.WriteString(ca.fg() + "● ")
			} else if i == a.round {
				rb.WriteString(ca.fg() + "◉ ")
			} else {
				rb.WriteString(t.Surface.fg() + "○ ")
			}
		}
		today := time.Now().Format("2006-01-02")
		sess, mins := 0, 0
		if a.cfg.StatsDate == today {
			sess, mins = a.cfg.StatsSessions, a.cfg.StatsMinutes
		}
		stats := fmt.Sprintf("  today %d sessions · %dh%02dm", sess, mins/60, mins%60)
		rb.WriteString(t.Muted.fg() + stats + reset)
		lines = append(lines, line{rb.String(), a.cfg.Rounds*2 + utf8.RuneCountInString(stats)})

		if a.flash != "" && now.Before(a.flashTill) {
			lines = append(lines, line{}, plain(t.Text, a.flash))
		}
		lines = append(lines, line{})
		switch {
		case a.mode == modeTodo || a.mode == modeInput:
			lines = append(lines, a.todoCard(w)...)
		case a.mode == modePicker:
			lines = append(lines, a.pickerCard(w)...)
		case a.cfg.Keys:
			lines = append(lines, a.legend(w)...)
		default:
			lines = append(lines, line{t.Text.fg() + "k" + reset + t.Muted.fg() + "  show shortcuts" + reset, 17})
		}
	}

	return a.compose(w, h, lines, clockFrom, clockTo, now)
}

// compose paints the animated background and overlays the UI lines centered on top.
func (a *App) compose(w, h int, lines []line, clockFrom, clockTo int, now time.Time) string {
	t := a.th()
	ca, cb := a.colors()
	cv := newCanvas(w, h)
	eff := effectNames[a.bgEff]
	if eff == "auto" {
		eff = t.Scene
	}
	if eff != a.bg.name || w != a.bg.w || h != a.bg.h {
		a.bg.reset(eff, w, h)
	}
	dt := 0.0
	if !a.lastFrame.IsZero() {
		dt = math.Min(now.Sub(a.lastFrame).Seconds(), 0.25)
	}
	a.lastFrame = now
	top := max((h-len(lines))/2, 0)
	prog := 0.0
	if a.total > 0 {
		prog = math.Min(math.Max(1-float64(a.left())/float64(a.total), 0), 1)
	}
	lo := layout{clockTop: top + clockFrom, clockBottom: top + clockTo, horizon: top + clockTo + 2, progress: prog}
	prefix := ""
	if o := a.imageOptions(); a.sharp.kind != sharpNone {
		// real picture behind the text (Kitty, Ghostty, iTerm2)
		prefix = a.sharp.update(&a.img, o, w, h)
		if a.sharp.errMsg != "" && a.imgErr != a.sharp.errMsg {
			a.imgErr = a.sharp.errMsg
			a.setFlash("background image: " + a.sharp.errMsg)
		}
	} else if o.Path != "" {
		if err := a.img.draw(cv, o); err != nil && a.imgErr != err.Error() {
			a.imgErr = err.Error()
			a.setFlash("background image: " + err.Error())
		}
	}
	a.bg.update(dt)
	a.bg.draw(cv, t, ca, cb, lo)

	for i, l := range lines {
		y := top + i
		if y >= h {
			break
		}
		if l.w == 0 {
			continue
		}
		pad := max((w-l.w)/2, 0)
		clockRow := i >= clockFrom && i < clockTo
		if !clockRow { // text rows get a calm strip behind them for readability
			for x := pad - 2; x < pad+l.w+2; x++ {
				cv.clear(x, y)
			}
		}
		overlay(cv, pad, y, l.s)
	}

	var sb strings.Builder
	sb.Grow(w * h * 4)
	sb.WriteString("\x1b[H")
	var cur, curBg rgb
	curOn, curBold, curHasBg := false, false, false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cv.cells[y*w+x]
			if c.hasBg != curHasBg || (c.hasBg && c.bg != curBg) {
				if c.hasBg {
					sb.WriteString(c.bg.bgSeq())
				} else {
					sb.WriteString("\x1b[49m")
				}
				curBg, curHasBg = c.bg, c.hasBg
			}
			if !c.on {
				sb.WriteByte(' ')
				continue
			}
			if c.bold != curBold {
				if c.bold {
					sb.WriteString(bold)
				} else {
					sb.WriteString("\x1b[22m")
				}
				curBold = c.bold
			}
			if !curOn || c.c != cur {
				sb.WriteString(c.c.fg())
				cur, curOn = c.c, true
			}
			sb.WriteRune(c.ch)
		}
		if y < h-1 {
			sb.WriteString("\r\n")
		}
	}
	sb.WriteString(reset)
	return prefix + sb.String()
}

// overlay writes an ANSI-colored string onto the canvas; plain spaces stay transparent.
func overlay(cv *canvas, x0, y int, s string) {
	x := x0
	var col, bgc rgb
	isBold, hasBg := false, false
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == 0x1b && i+1 < len(rs) && rs[i+1] == '[' {
			j := i + 2
			for j < len(rs) && rs[j] != 'm' {
				j++
			}
			params := strings.Split(string(rs[i+2:j]), ";")
			for k := 0; k < len(params); k++ {
				switch params[k] {
				case "0", "":
					isBold, col, hasBg = false, rgb{}, false
				case "49":
					hasBg = false
				case "48":
					if k+4 < len(params) && params[k+1] == "2" {
						rr, _ := strconv.Atoi(params[k+2])
						gg, _ := strconv.Atoi(params[k+3])
						bb, _ := strconv.Atoi(params[k+4])
						bgc, hasBg = rgb{rr, gg, bb}, true
						k += 4
					}
				case "1":
					isBold = true
				case "22":
					isBold = false
				case "38":
					if k+4 < len(params) && params[k+1] == "2" {
						rr, _ := strconv.Atoi(params[k+2])
						gg, _ := strconv.Atoi(params[k+3])
						bb, _ := strconv.Atoi(params[k+4])
						col = rgb{rr, gg, bb}
						k += 4
					}
				}
			}
			i = j
			continue
		}
		if x >= 0 && x < cv.w && y >= 0 && y < cv.h {
			old := cv.cells[y*cv.w+x]
			switch {
			case hasBg: // explicit background: opaque, even for spaces
				cv.cells[y*cv.w+x] = cell{ch: r, c: col, bold: isBold, on: true, bg: bgc, hasBg: true}
			case r != ' ':
				cv.cells[y*cv.w+x] = cell{ch: r, c: col, bold: isBold, on: true, bg: old.bg, hasBg: old.hasBg}
			}
		}
		x++
	}
}

// ---------- main ----------

func usage() {
	fmt.Fprintf(os.Stderr, `SuperPomodoro %s — a pretty Pomodoro timer for your terminal

Usage: superpomodoro [flags]
       superpomodoro themegen            open the theme editor in your browser
       superpomodoro theme list|new|rm|restore|dir  manage theme files
       superpomodoro todo [add|done|rm]  manage the to-do list

Flags:
`, version)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nThemes: %s\nFonts:  %s\nBars:   %s\nBg:     %s\nConfig: %s\n",
		names(func(i int) string { return themes[i].Name }, len(themes)),
		names(func(i int) string { return fonts[i].Name }, len(fonts)),
		names(func(i int) string { return bars[i].Name }, len(bars)),
		strings.Join(effectNames, ", "),
		configPath())
}

func names(f func(int) string, n int) string {
	s := make([]string, n)
	for i := range s {
		s[i] = f(i)
	}
	return strings.Join(s, ", ")
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "theme", "themes":
			os.Exit(themeCommand(os.Args[2:]))
		case "themegen":
			os.Exit(themegenCommand(os.Args[2:]))
		case "todo", "todos":
			os.Exit(todoCommand(os.Args[2:]))
		}
	}
	themeErr := error(nil)
	themes, themeErr = loadThemes()
	cfg := loadConfig()
	work := flag.Int("work", int(cfg.Work.Minutes()), "focus minutes")
	short := flag.Int("short", int(cfg.Short.Minutes()), "short break minutes")
	long := flag.Int("long", int(cfg.Long.Minutes()), "long break minutes")
	rounds := flag.Int("rounds", cfg.Rounds, "focus rounds before a long break")
	theme := flag.String("theme", cfg.Theme, "color theme")
	font := flag.String("font", cfg.Font, "digit font")
	bar := flag.String("bar", cfg.Bar, "progress bar style")
	auto := flag.Bool("auto", cfg.Auto, "start the next phase automatically")
	bell := flag.Bool("bell", cfg.Bell, "ring the terminal bell at the end of a phase")
	noNotify := flag.Bool("no-notify", !cfg.Notify, "disable desktop notifications")
	zen := flag.Bool("zen", cfg.Zen, "minimal view, clock and bar only")
	noSound := flag.Bool("no-sound", !cfg.Sound, "disable the sound at the end of a phase")
	bgFlag := flag.String("bg", cfg.Bg, "background animation (auto follows the theme)")
	imageFlag := flag.String("image", "", "background image (png, jpg or gif) for this run")
	showVersion := flag.Bool("version", false, "print version")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println("superpomodoro", version)
		return
	}
	cfg.Work = time.Duration(max(*work, 1)) * time.Minute
	cfg.Short = time.Duration(max(*short, 1)) * time.Minute
	cfg.Long = time.Duration(max(*long, 1)) * time.Minute
	cfg.Rounds = max(*rounds, 1)
	cfg.Auto, cfg.Bell, cfg.Notify, cfg.Zen, cfg.Sound = *auto, *bell, !*noNotify, *zen, !*noSound

	a := &App{cfg: cfg, round: 1, theme: themeIndex(*theme), font: fontIndex(*font), bar: barIndex(*bar), bgEff: effectIndex(*bgFlag), todos: loadTodos(), todoStamp: todoStamp(), sharp: sharpLayer{kind: detectSharp(cfg.ImageMode)}, imageFlag: *imageFlag, themeStamp: themeFilesStamp(), start: time.Now()}
	if themeErr != nil {
		a.setFlash("theme file: " + themeErr.Error())
	}
	a.setPhase(Focus)

	restore, err := enterRaw()
	if err != nil {
		fmt.Fprintln(os.Stderr, "superpomodoro needs an interactive terminal:", err)
		os.Exit(1)
	}
	quit := func() {
		a.sharp.cleanup()
		restore()
		if a.mini {
			a.mini = false
			a.writeMiniState()
		}
		// persist look and stats only; CLI flags stay one-off
		saved := loadConfig()
		saved.Theme, saved.Font, saved.Bar = themes[a.theme].Name, fonts[a.font].Name, bars[a.bar].Name
		saved.Keys, saved.Sound, saved.Bg = a.cfg.Keys, a.cfg.Sound, effectNames[a.bgEff]
		saved.StatsDate, saved.StatsSessions, saved.StatsMinutes = a.cfg.StatsDate, a.cfg.StatsSessions, a.cfg.StatsMinutes
		saveConfig(saved)
		os.Exit(0)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	keys := make(chan key, 64)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				close(keys)
				return
			}
			for _, k := range parseKeys(buf[:n]) {
				keys <- k
			}
		}
	}()

	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	os.Stdout.WriteString("\x1b[2J")
	for {
		if a.running && a.left() <= 0 {
			a.running = false
			a.finish()
		}
		a.reloadThemesIfChanged()
		a.reloadTodosIfChanged()
		if a.mini {
			a.pollMiniCommands()
			if time.Since(a.lastMiniWrite) > 200*time.Millisecond {
				a.writeMiniState()
			}
		}
		os.Stdout.WriteString(a.frame())

		select {
		case <-sig:
			quit()
		case k, ok := <-keys:
			if !ok {
				quit()
			}
			a.handleKey(k, quit)
		case <-tick.C:
		}
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (b *background) nameOr(def string) string {
	if b.name == "" {
		return def
	}
	return b.name
}

// th returns the active theme adapted to the terminal background (cached).
func (a *App) th() Theme {
	img, _ := a.imageSetting()
	hasImg := img != "" && a.img.err == nil
	if a.thCache == nil || a.thFor != a.theme || a.thImg != hasImg {
		bg, known := termBg, termBgKnown
		if a.cfg.TermBg != "" {
			bg, known = hex(a.cfg.TermBg), true
		}
		if hasImg { // text sits on darkened image strips
			bg, known = rgb{14, 14, 18}, true
		}
		a.thImg = hasImg
		t := adapt(themes[a.theme], bg, known)
		a.thCache, a.thFor = &t, a.theme
	}
	return *a.thCache
}
