package main

import "math"

// Readability: theme colors are tuned for dark terminals. When the real terminal
// background is known, text colors are nudged until they reach a minimum contrast
// ratio against it, so they stay readable on any background while keeping their hue.

func channel(v int) float64 {
	c := float64(v) / 255
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func luminance(c rgb) float64 {
	return 0.2126*channel(c.r) + 0.7152*channel(c.g) + 0.0722*channel(c.b)
}

func contrast(a, b rgb) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ensureContrast moves c toward white (dark bg) or black (light bg) until it reaches min.
func ensureContrast(c, bg rgb, min float64) rgb {
	if contrast(c, bg) >= min {
		return c
	}
	target := rgb{255, 255, 255}
	if luminance(bg) > 0.4 {
		target = rgb{0, 0, 0}
	}
	for t := 0.05; t <= 1.0001; t += 0.05 {
		n := lerp(c, target, t)
		if contrast(n, bg) >= min {
			return n
		}
	}
	return target
}

// adapt returns the theme tuned to the terminal background.
func adapt(t Theme, bg rgb, known bool) Theme {
	if !known {
		t.Base = t.Surface
		return t
	}
	t.Base = bg
	t.Text = ensureContrast(t.Text, bg, 7)
	t.Muted = ensureContrast(t.Muted, bg, 4.5)
	t.Surface = ensureContrast(t.Surface, bg, 1.8)
	for _, c := range []*rgb{&t.FocusA, &t.FocusB, &t.BreakA, &t.BreakB, &t.LongA, &t.LongB} {
		*c = ensureContrast(*c, bg, 3)
	}
	return t
}
