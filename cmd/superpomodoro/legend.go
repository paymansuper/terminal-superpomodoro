package main

import (
	"strings"
	"unicode/utf8"
)

// The shortcut panel: three groups (timer, look, view) on an opaque card, with
// the current setting of every option shown as a colored chip.
//
//   ╭──────────────────────────────────────────────────────────────────────╮
//   │  TIMER                   LOOK                     VIEW               │
//   │  space  pause            t  theme  ▌synthwave▐    z  zen mode        │
//   │  ...                                                                 │
//   ╰──────────────────────────────────────────────────────────────────────╯

type seg struct {
	sb strings.Builder
	w  int
	bg rgb
}

func (s *seg) text(str string, fg rgb, bold bool) {
	s.sb.WriteString(s.bg.bgSeq() + fg.fg())
	if bold {
		s.sb.WriteString("\x1b[1m")
	}
	s.sb.WriteString(str + reset)
	s.w += utf8.RuneCountInString(str)
}

// cell writes str padded to width w.
func (s *seg) cell(str string, w int, fg rgb, bold bool) {
	if n := utf8.RuneCountInString(str); n > w {
		str = string([]rune(str)[:w])
	}
	s.text(str+strings.Repeat(" ", w-utf8.RuneCountInString(str)), fg, bold)
}

// chip draws a value as a uniform pill of width w-1 on its own background.
func (s *seg) chip(val string, w int, chipBg, chipFg rgb) {
	inner := w - 3
	if utf8.RuneCountInString(val) > inner {
		val = string([]rune(val)[:inner-1]) + "…"
	}
	label := " " + val + strings.Repeat(" ", inner-utf8.RuneCountInString(val)) + " "
	s.sb.WriteString(chipBg.bgSeq() + chipFg.fg() + "\x1b[1m" + label + reset)
	s.w += utf8.RuneCountInString(label)
	s.text(" ", chipFg, false)
}

type entry struct {
	key, label, value string
	active            bool // value chip highlighted in the phase color
}

func (a *App) legend(screenW int) []line {
	t := a.th()
	ca, _ := a.colors()

	panel := lerp(t.Surface, rgb{0, 0, 0}, 0.5)
	if termBgKnown || a.cfg.TermBg != "" {
		panel = lerp(t.Base, rgb{0, 0, 0}, 0.4)
	}
	if a.thImg {
		panel = rgb{14, 14, 18}
	}
	border := lerp(panel, t.Muted, 0.55)
	chipOff := lerp(panel, t.Muted, 0.35)

	pause := "start"
	if a.running {
		pause = "pause"
	}
	scene := effectNames[a.bgEff]
	if scene == "auto" {
		scene = a.bg.nameOr("off")
	}
	img, imgOn := "none", false
	if p, _ := a.imageSetting(); p != "" {
		img, imgOn = "pixel", true
		if a.sharp.kind != sharpNone {
			img = "sharp"
		}
	} else if a.imageOff {
		img = "off"
	}

	timer := []entry{
		{"space", pause, "", false},
		{"s", "skip phase", "", false},
		{"r", "restart phase", "", false},
		{"+ / -", "one minute", "", false},
		{"l", "to-do list", "", false},
	}
	look := []entry{
		{"t", "theme", themes[a.theme].Name, true},
		{"f", "font", fonts[a.font].Name, true},
		{"b", "bar", bars[a.bar].Name, true},
		{"g", "scene", scene, scene != "off"},
		{"i", "image", img, imgOn},
	}
	view := []entry{
		{"m", "sound", onOff(a.cfg.Sound), a.cfg.Sound},
		{"w", "mini window", onOff(a.mini), a.mini},
		{"z", "zen mode", "", false},
		{"k", "hide keys", "", false},
		{"q", "quit", "", false},
	}

	const (
		c1Key, c1Label = 6, 14
		c2Key, c2Label = 3, 7
		c2Chip         = 14
		c3Key, c3Label = 3, 13
		c3Chip         = 6
		gap            = "   "
	)
	inner := 2 + (c1Key + c1Label) + len(gap) + (c2Key + c2Label + c2Chip) + len(gap) + (c3Key + c3Label + c3Chip) + 2
	if inner+2 > screenW-2 {
		// not enough room for the card: a single hint line
		return []line{plain(t.Muted, "space start/pause · s skip · t theme · k keys · q quit")}
	}

	row := func(build func(s *seg)) line {
		s := &seg{bg: panel}
		s.text("│", border, false)
		build(s)
		s.text("│", border, false)
		return line{s.sb.String(), s.w}
	}
	edge := func(l, r string) line {
		s := &seg{bg: panel}
		s.text(l+strings.Repeat("─", inner)+r, border, false)
		return line{s.sb.String(), s.w}
	}
	put := func(s *seg, e entry, keyW, labelW, chipW int) {
		if e.key == "" {
			s.cell("", keyW+labelW+chipW, t.Text, false)
			return
		}
		s.cell(e.key, keyW, ca, true)
		s.cell(e.label, labelW, t.Text, false)
		if chipW == 0 {
			return
		}
		if e.value == "" {
			s.cell("", chipW, t.Text, false)
		} else if e.active {
			s.chip(e.value, chipW, ca, panel)
		} else {
			s.chip(e.value, chipW, chipOff, t.Text)
		}
	}

	out := []line{edge("╭", "╮")}
	out = append(out, row(func(s *seg) {
		s.cell("  TIMER", 2+c1Key+c1Label, t.Muted, true)
		s.cell(gap+"LOOK", len(gap)+c2Key+c2Label+c2Chip, t.Muted, true)
		s.cell(gap+"VIEW", len(gap)+c3Key+c3Label+c3Chip+2, t.Muted, true)
	}))
	rows := max(len(timer), len(look), len(view))
	at := func(list []entry, i int) entry {
		if i < len(list) {
			return list[i]
		}
		return entry{}
	}
	for i := 0; i < rows; i++ {
		e1, e2, e3 := at(timer, i), at(look, i), at(view, i)
		out = append(out, row(func(s *seg) {
			s.cell("", 2, t.Text, false)
			put(s, e1, c1Key, c1Label, 0)
			s.cell(gap, len(gap), t.Text, false)
			put(s, e2, c2Key, c2Label, c2Chip)
			s.cell(gap, len(gap), t.Text, false)
			put(s, e3, c3Key, c3Label, c3Chip)
			s.cell("", 2, t.Text, false)
		}))
	}
	return append(out, edge("╰", "╯"))
}
