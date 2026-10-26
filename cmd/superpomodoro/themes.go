package main

import (
	"fmt"
	"strconv"
	"strings"
)

type rgb struct{ r, g, b int }

func hex(s string) rgb {
	s = strings.TrimPrefix(s, "#")
	v, _ := strconv.ParseUint(s, 16, 32)
	return rgb{int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)}
}

func (c rgb) fg() string    { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b) }
func (c rgb) bgSeq() string { return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.r, c.g, c.b) }

func lerp(a, b rgb, t float64) rgb {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return rgb{
		a.r + int(float64(b.r-a.r)*t),
		a.g + int(float64(b.g-a.g)*t),
		a.b + int(float64(b.b-a.b)*t),
	}
}

type Theme struct {
	Name                 string
	FocusA, FocusB       rgb
	BreakA, BreakB       rgb
	LongA, LongB         rgb
	Text, Muted, Surface rgb
	Base                 rgb     // what background effects fade into
	Scene                string  // animated scene for "auto": rain, waves, grid or off
	Image                string  // optional background image for this theme
	ImageBrightness      float64 // 0..1, 0 means use the global setting
	ImageFit             string  // auto, cover, contain; empty means global setting
	ImageScale           float64 // contain size 0..1
	ImagePosition        string  // top, center, bottom
	Custom               bool    // loaded from a theme file
	Extends              string  // base theme named in the theme file
	Author, Description  string
}

func mk(name, fa, fb, ba, bb, la, lb, text, muted, surface string) Theme {
	return Theme{Name: name, FocusA: hex(fa), FocusB: hex(fb), BreakA: hex(ba), BreakB: hex(bb),
		LongA: hex(la), LongB: hex(lb), Text: hex(text), Muted: hex(muted), Surface: hex(surface)}
}

var builtinThemes = []Theme{
	mk("tokyo-night", "#f7768e", "#bb9af7", "#7dcfff", "#9ece6a", "#7aa2f7", "#bb9af7", "#c0caf5", "#565f89", "#292e42"),
	mk("catppuccin", "#f38ba8", "#fab387", "#94e2d5", "#89b4fa", "#cba6f7", "#89dceb", "#cdd6f4", "#6c7086", "#313244"),
	mk("dracula", "#ff79c6", "#bd93f9", "#8be9fd", "#50fa7b", "#bd93f9", "#8be9fd", "#f8f8f2", "#6272a4", "#44475a"),
	mk("kanagawa", "#e46876", "#ffa066", "#7fb4ca", "#98bb6c", "#957fb8", "#7aa89f", "#dcd7ba", "#727169", "#2a2a37"),
	mk("synthwave", "#ff2a6d", "#ff9e00", "#05d9e8", "#d1f7ff", "#b967ff", "#05d9e8", "#f5f5ff", "#7a5c9e", "#2b213a"),
	mk("matrix", "#00ff41", "#008f11", "#00ff9c", "#00b36b", "#39ff14", "#00ff9c", "#b6ffb6", "#1f6b2c", "#0b2410"),
	mk("mono", "#ffffff", "#8a8a8a", "#d0d0d0", "#7a7a7a", "#e0e0e0", "#6a6a6a", "#e6e6e6", "#5c5c5c", "#2a2a2a"),
}

// themes is the active list: built-ins plus theme files (see themefile.go).
var themes = withScenes(builtinThemes)

func withScenes(ts []Theme) []Theme {
	out := make([]Theme, len(ts))
	copy(out, ts)
	for i := range out {
		if out[i].Scene == "" {
			out[i].Scene = themeEffect[out[i].Name]
			if out[i].Scene == "" {
				out[i].Scene = "off"
			}
		}
	}
	return out
}

func themeIndex(name string) int {
	for i, t := range themes {
		if t.Name == name {
			return i
		}
	}
	return 0
}

// Digit fonts: 5 rows, 3 cells per glyph; each cell rendered with the font's "ink".
var glyphs = map[rune][5]string{
	'0': {"###", "#.#", "#.#", "#.#", "###"},
	'1': {".#.", "##.", ".#.", ".#.", "###"},
	'2': {"###", "..#", "###", "#..", "###"},
	'3': {"###", "..#", ".##", "..#", "###"},
	'4': {"#.#", "#.#", "###", "..#", "..#"},
	'5': {"###", "#..", "###", "..#", "###"},
	'6': {"###", "#..", "###", "#.#", "###"},
	'7': {"###", "..#", ".#.", ".#.", ".#."},
	'8': {"###", "#.#", "###", "#.#", "###"},
	'9': {"###", "#.#", "###", "..#", "###"},
	':': {".", "#", ".", "#", "."},
}

type Font struct {
	Name string
	Ink  string // two terminal columns per cell
	Fill string // background cell (two columns), usually spaces
}

var fonts = []Font{
	{"block", "██", "  "},
	{"shade", "▓▓", "░░"},
	{"pixel", "■ ", "  "},
	{"dots", "● ", "  "},
	{"ghost", "██", "··"},
	{"ascii", "##", "  "},
}

func fontIndex(name string) int {
	for i, f := range fonts {
		if f.Name == name {
			return i
		}
	}
	return 0
}

type BarStyle struct {
	Name        string
	Full, Empty string
	Partials    []string // optional sub-cell resolution
}

var bars = []BarStyle{
	{"smooth", "█", "░", []string{"▏", "▎", "▍", "▌", "▋", "▊", "▉"}},
	{"line", "━", "─", nil},
	{"dots", "●", "·", nil},
	{"blocks", "■", "□", nil},
	{"wave", "▰", "▱", nil},
}

func barIndex(name string) int {
	for i, b := range bars {
		if b.Name == name {
			return i
		}
	}
	return 0
}
