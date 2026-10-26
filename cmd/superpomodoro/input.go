package main

// Keyboard input and the interactive panels: the to-do list, the text input
// for a task and the picker used for theme, font, bar and scene.

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type keyCode int

const (
	kRune keyCode = iota
	kUp
	kDown
	kLeft
	kRight
	kEnter
	kEsc
	kBackspace
	kTab
	kCtrlC
)

type key struct {
	code keyCode
	r    rune
}

// parseKeys turns raw terminal bytes into key events (arrows, enter, UTF-8 text).
func parseKeys(buf []byte) []key {
	var out []key
	for i := 0; i < len(buf); {
		b := buf[i]
		switch {
		case b == 0x1b:
			if i+2 < len(buf) && (buf[i+1] == '[' || buf[i+1] == 'O') {
				code := map[byte]keyCode{'A': kUp, 'B': kDown, 'C': kRight, 'D': kLeft}[buf[i+2]]
				if code != 0 {
					out = append(out, key{code: code})
				}
				// skip the rest of the sequence
				j := i + 2
				for j < len(buf) && !(buf[j] >= '@' && buf[j] <= '~') {
					j++
				}
				i = j + 1
				continue
			}
			if i+1 < len(buf) {
				i += 2 // alt+key, ignore
				continue
			}
			out = append(out, key{code: kEsc})
			i++
		case b == '\r' || b == '\n':
			out = append(out, key{code: kEnter})
			i++
		case b == 0x7f || b == 0x08:
			out = append(out, key{code: kBackspace})
			i++
		case b == '\t':
			out = append(out, key{code: kTab})
			i++
		case b == 3:
			out = append(out, key{code: kCtrlC})
			i++
		case b < 0x20:
			i++ // other control keys
		default:
			r, n := utf8.DecodeRune(buf[i:])
			out = append(out, key{code: kRune, r: r})
			i += n
		}
	}
	return out
}

type uiMode int

const (
	modeNormal uiMode = iota
	modeTodo
	modeInput
	modePicker
)

type picker struct {
	title   string
	options []string
	sel     int
	orig    int
	apply   func(int)
	swatch  func(int) []rgb // optional color preview per option
}

// ---------- key handling ----------

func (a *App) handleKey(k key, quit func()) {
	if k.code == kCtrlC {
		quit()
	}
	switch a.mode {
	case modeTodo:
		a.handleTodoKey(k)
	case modeInput:
		a.handleInputKey(k)
	case modePicker:
		a.handlePickerKey(k)
	default:
		a.handleNormalKey(k, quit)
	}
}

func (a *App) handleNormalKey(k key, quit func()) {
	if k.code != kRune {
		return
	}
	switch k.r {
	case 'q', 'Q':
		quit()
	case ' ', 'p':
		a.toggle()
	case 's', 'n':
		a.advance(false)
	case 'r':
		a.setPhase(a.phase)
		a.ready = false
	case '+', '=':
		a.adjust(time.Minute)
	case '-', '_':
		a.adjust(-time.Minute)
	case 't', 'T':
		a.openThemePicker()
	case 'f':
		a.openPicker("FONT", len(fonts), a.font, func(i int) string { return fonts[i].Name },
			func(i int) { a.font = i }, nil)
	case 'b':
		a.openPicker("PROGRESS BAR", len(bars), a.bar, func(i int) string { return bars[i].Name },
			func(i int) { a.bar = i }, nil)
	case 'g':
		a.openPicker("SCENE", len(effectNames), a.bgEff, func(i int) string {
			if effectNames[i] == "auto" {
				return "auto · " + themes[a.theme].Scene
			}
			return effectNames[i]
		}, func(i int) { a.bgEff = i }, nil)
	case 'l', 'L':
		a.mode = modeTodo
		if len(a.todos) == 0 {
			a.startInput(-1)
		}
	case 'w':
		a.toggleMini()
	case 'i':
		a.imageOff = !a.imageOff
		if p, _ := a.imageSetting(); p == "" && !a.imageOff {
			a.setFlash("no background image set, see README")
		} else if a.imageOff {
			a.setFlash("background image off")
		} else {
			a.setFlash("background image on")
		}
	case 'm':
		a.cfg.Sound = !a.cfg.Sound
		if a.cfg.Sound {
			a.setFlash("sound on")
			a.playSound(a.cfg.SoundFocus)
		} else {
			a.setFlash("sound off")
		}
	case 'z':
		a.cfg.Zen = !a.cfg.Zen
	case 'k', '?', 'h':
		a.cfg.Keys = !a.cfg.Keys
	}
}

func (a *App) openPicker(title string, n, cur int, name func(int) string, apply func(int), swatch func(int) []rgb) {
	opts := make([]string, n)
	for i := range opts {
		opts[i] = name(i)
	}
	a.picker = &picker{title: title, options: opts, sel: cur, orig: cur, apply: apply, swatch: swatch}
	a.mode = modePicker
}

func (a *App) openThemePicker() {
	a.openPicker("THEME", len(themes), a.theme, func(i int) string {
		if themes[i].Custom {
			return themes[i].Name + " ·"
		}
		return themes[i].Name
	}, func(i int) { a.theme = i; a.thCache = nil }, func(i int) []rgb {
		t := themes[i]
		return []rgb{t.FocusA, t.FocusB, t.BreakA, t.LongA}
	})
}

func (a *App) handlePickerKey(k key) {
	p := a.picker
	move := func(d int) {
		p.sel = (p.sel + d + len(p.options)) % len(p.options)
		p.apply(p.sel) // live preview
	}
	switch {
	case k.code == kUp || k.r == 'k':
		move(-1)
	case k.code == kDown || k.r == 'j' || k.code == kTab:
		move(1)
	case k.code == kEnter || k.r == ' ':
		a.mode, a.picker = modeNormal, nil
		a.setFlash(strings.ToLower(p.title) + " · " + strings.TrimSuffix(p.options[p.sel], " ·"))
	case k.code == kEsc || k.r == 'q':
		p.apply(p.orig)
		a.mode, a.picker = modeNormal, nil
	case k.code == kRune:
		// jump to the next option starting with that letter
		for d := 1; d <= len(p.options); d++ {
			i := (p.sel + d) % len(p.options)
			if strings.HasPrefix(p.options[i], string(k.r)) {
				p.sel = i
				p.apply(i)
				break
			}
		}
	}
}

func (a *App) handleTodoKey(k key) {
	n := len(a.todos)
	switch {
	case k.code == kEsc || k.r == 'l' || k.r == 'L' || k.r == 'q':
		a.mode = modeNormal
	case k.code == kUp || k.r == 'k':
		if n > 0 {
			a.todoSel = (a.todoSel - 1 + n) % n
		}
	case k.code == kDown || k.r == 'j':
		if n > 0 {
			a.todoSel = (a.todoSel + 1) % n
		}
	case k.r == 'a' || k.r == 'n' || k.r == '+':
		a.startInput(-1)
	case k.r == 'e' && n > 0:
		a.startInput(a.todoSel)
	case k.code == kEnter && n > 0:
		a.setCurrent(a.todoSel)
	case (k.r == 'x' || k.r == ' ') && n > 0:
		t := &a.todos[a.todoSel]
		t.Done = !t.Done
		if t.Done {
			t.Current = false
		}
		a.saveTodos()
	case (k.r == 'd' || k.code == kBackspace) && n > 0:
		a.todos = append(a.todos[:a.todoSel], a.todos[a.todoSel+1:]...)
		a.todoSel = min(a.todoSel, max(len(a.todos)-1, 0))
		a.saveTodos()
	case k.r == 'c':
		var keep []todo
		for _, t := range a.todos {
			if !t.Done {
				keep = append(keep, t)
			}
		}
		if removed := len(a.todos) - len(keep); removed > 0 {
			a.todos, a.todoSel = keep, 0
			a.saveTodos()
			a.setFlash(fmt.Sprintf("cleared %d done", removed))
		}
	case k.r == 'K' && a.todoSel > 0: // move up
		a.todos[a.todoSel], a.todos[a.todoSel-1] = a.todos[a.todoSel-1], a.todos[a.todoSel]
		a.todoSel--
		a.saveTodos()
	case k.r == 'J' && a.todoSel < n-1: // move down
		a.todos[a.todoSel], a.todos[a.todoSel+1] = a.todos[a.todoSel+1], a.todos[a.todoSel]
		a.todoSel++
		a.saveTodos()
	case k.r == 's' || k.r == 'p':
		if k.r == 's' {
			a.advance(false)
		} else {
			a.toggle()
		}
	}
}

func (a *App) startInput(edit int) {
	a.inputEdit = edit
	a.input = nil
	if edit >= 0 {
		a.input = []rune(a.todos[edit].Text)
	}
	a.mode = modeInput
}

func (a *App) handleInputKey(k key) {
	switch k.code {
	case kEsc:
		a.mode = modeTodo
	case kEnter:
		text := strings.TrimSpace(string(a.input))
		if text != "" {
			if a.inputEdit >= 0 && a.inputEdit < len(a.todos) {
				a.todos[a.inputEdit].Text = text
			} else {
				a.todos = append(a.todos, todo{Text: text})
				a.todoSel = len(a.todos) - 1
			}
			a.saveTodos()
		}
		a.mode = modeTodo
	case kBackspace:
		if len(a.input) > 0 {
			a.input = a.input[:len(a.input)-1]
		}
	case kRune:
		if len(a.input) < 120 {
			a.input = append(a.input, k.r)
		}
	}
}

// ---------- panels ----------

// card renders rows inside the same rounded card as the shortcut legend.
func (a *App) card(inner int, rows []func(s *seg)) []line {
	t := a.th()
	panel, border := a.panelColors()
	_ = t
	edge := func(l, r string) line {
		s := &seg{bg: panel}
		s.text(l+strings.Repeat("─", inner)+r, border, false)
		return line{s.sb.String(), s.w}
	}
	out := []line{edge("╭", "╮")}
	for _, build := range rows {
		s := &seg{bg: panel}
		s.text("│", border, false)
		start := s.w
		build(s)
		if used := s.w - start; used < inner {
			s.cell("", inner-used, t.Text, false)
		}
		s.text("│", border, false)
		out = append(out, line{s.sb.String(), s.w})
	}
	return append(out, edge("╰", "╯"))
}

func (a *App) panelColors() (panel, border rgb) {
	t := a.th()
	panel = lerp(t.Surface, rgb{0, 0, 0}, 0.5)
	if termBgKnown || a.cfg.TermBg != "" {
		panel = lerp(t.Base, rgb{0, 0, 0}, 0.4)
	}
	if a.thImg {
		panel = rgb{14, 14, 18}
	}
	return panel, lerp(panel, t.Muted, 0.55)
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:max(n-1, 0)]) + "…"
}

func (a *App) todoCard(screenW int) []line {
	t := a.th()
	ca, _ := a.colors()
	panel, _ := a.panelColors()
	inner := min(74, screenW-6)
	if inner < 30 {
		return []line{plain(t.Muted, "terminal too small for the to-do list")}
	}
	open, done := 0, 0
	for _, td := range a.todos {
		if td.Done {
			done++
		} else {
			open++
		}
	}
	var rows []func(s *seg)
	rows = append(rows, func(s *seg) {
		s.cell("  TO DO", 10, t.Muted, true)
		s.cell(fmt.Sprintf("%d open · %d done", open, done), inner-10, t.Muted, false)
	})
	const maxRows = 8
	first := 0
	if a.todoSel >= maxRows {
		first = a.todoSel - maxRows + 1
	}
	for i := first; i < len(a.todos) && i < first+maxRows; i++ {
		td, i := a.todos[i], i
		rows = append(rows, func(s *seg) {
			selected := i == a.todoSel && a.mode == modeTodo
			bgc := panel
			if selected {
				bgc = lerp(panel, ca, 0.22)
			}
			s.bg = bgc
			mark := "  "
			if selected {
				mark = "› "
			}
			s.cell(" "+mark, 3, ca, true)
			box, boxCol := "○", t.Muted
			if td.Done {
				box, boxCol = "●", ca
			}
			s.cell(box+" ", 2, boxCol, false)
			pom := ""
			if td.Pomos > 0 {
				pom = fmt.Sprintf("×%d", td.Pomos)
			}
			tag := ""
			if td.Current {
				tag = "FOCUS"
			}
			textW := inner - 3 - 2 - 8 - 5 - 1
			txtCol := t.Text
			if td.Done {
				txtCol = t.Muted
			}
			s.cell(truncate(td.Text, textW), textW, txtCol, td.Current)
			if tag != "" {
				s.chip(tag, 8, ca, panel)
			} else {
				s.cell("", 8, t.Text, false)
			}
			s.cell(pom, 5, ca, false)
			s.cell("", 1, t.Text, false)
			s.bg = panel
		})
	}
	if len(a.todos) == 0 && a.mode != modeInput {
		rows = append(rows, func(s *seg) { s.cell("   nothing yet, press a to add a task", inner, t.Muted, false) })
	}
	if a.mode == modeInput {
		label := "   + "
		if a.inputEdit >= 0 {
			label = "   ~ "
		}
		rows = append(rows, func(s *seg) {
			s.cell(label, 5, ca, true)
			txt := string(a.input)
			if r := []rune(txt); len(r) > inner-9 {
				txt = string(r[len(r)-(inner-9):])
			}
			s.text(txt, t.Text, false)
			s.text("█", ca, false)
		})
		rows = append(rows, func(s *seg) {}, func(s *seg) {
			s.cell("   return save · esc cancel", inner, t.Muted, false)
		})
	} else {
		rows = append(rows, func(s *seg) {}, func(s *seg) {
			s.cell("   a add · e edit · enter focus · x done · d delete · c clear · l close", inner, t.Muted, false)
		})
	}
	return a.card(inner, rows)
}

func (a *App) pickerCard(screenW int) []line {
	t := a.th()
	ca, _ := a.colors()
	panel, _ := a.panelColors()
	p := a.picker
	inner := 40
	var rows []func(s *seg)
	rows = append(rows, func(s *seg) { s.cell("  "+p.title, inner, t.Muted, true) })
	const maxRows = 9
	first := 0
	if p.sel >= maxRows {
		first = p.sel - maxRows + 1
	}
	for i := first; i < len(p.options) && i < first+maxRows; i++ {
		i := i
		rows = append(rows, func(s *seg) {
			selected := i == p.sel
			s.cell("  ", 2, t.Text, false)
			if p.swatch != nil {
				for _, c := range p.swatch(i) {
					s.text("█", c, false)
				}
				s.cell(" ", 2, t.Text, false)
			}
			width := inner - 4
			if p.swatch != nil {
				width -= 6
			}
			if selected {
				s.chip(p.options[i], width, ca, panel)
			} else {
				s.cell(" "+p.options[i], width, t.Text, false)
			}
		})
	}
	rows = append(rows, func(s *seg) {}, func(s *seg) {
		s.cell("  ↑↓ choose · return keep · esc cancel", inner, t.Muted, false)
	})
	return a.card(inner, rows)
}
