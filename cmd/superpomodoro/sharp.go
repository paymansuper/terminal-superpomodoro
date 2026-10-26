package main

// Sharp background images, for terminals that can show real pictures:
//
//   - Kitty and Ghostty: the Kitty graphics protocol. The picture is placed
//     below the text and below cells with their own background color, so the
//     timer and the shortcut card stay on top.
//   - iTerm2: its own escape sequence that sets the session background image.
//
// Everywhere else the half block renderer in image.go is used.

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type sharpKind int

const (
	sharpNone sharpKind = iota
	sharpKitty
	sharpITerm
)

const kittyImageID = 7351

// detectSharp decides how images can be shown in this terminal.
// image_mode = "pixel" forces the half block fallback, "sharp" forces Kitty graphics.
func detectSharp(mode string) sharpKind {
	switch mode {
	case "pixel":
		return sharpNone
	case "sharp", "kitty":
		return sharpKitty
	case "iterm":
		return sharpITerm
	}
	if os.Getenv("TMUX") != "" || strings.HasPrefix(os.Getenv("TERM"), "screen") {
		return sharpNone // multiplexers swallow graphics escapes
	}
	term, prog := os.Getenv("TERM"), os.Getenv("TERM_PROGRAM")
	switch {
	case prog == "ghostty", term == "xterm-ghostty", os.Getenv("GHOSTTY_RESOURCES_DIR") != "":
		return sharpKitty
	case term == "xterm-kitty", os.Getenv("KITTY_WINDOW_ID") != "":
		return sharpKitty
	case prog == "iTerm.app":
		return sharpITerm
	}
	return sharpNone
}

// termPixels returns the window size in pixels, or a guess if the terminal doesn't say.
func termPixels(cols, rows int) (int, int) {
	ws := &winsize{}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(ws)))
	if errno != 0 || ws.X == 0 || ws.Y == 0 {
		return cols * 10, rows * 20
	}
	return int(ws.X), int(ws.Y)
}

type sharpLayer struct {
	kind   sharpKind
	key    string // what is currently shown
	file   string
	shown  bool
	errMsg string
}

// renderFile scales the picture to the window (cover), applies the brightness
// and writes it as PNG into the cache, which the terminal then reads.
func renderSharpFile(src image.Image, pw, ph int, bright float64, key string) (string, error) {
	const maxW = 2560
	if pw > maxW {
		ph = ph * maxW / pw
		pw = maxW
	}
	b := src.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	scale := max(float64(pw)/iw, float64(ph)/ih)
	offX, offY := (iw-float64(pw)/scale)/2, (ih-float64(ph)/scale)/2
	out := image.NewRGBA(image.Rect(0, 0, pw, ph))
	step := 1 / scale
	samples := 1
	if step > 1.5 {
		samples = 2 // average a few source pixels when shrinking
	}
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			var r, g, bl, n float64
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					fx := offX + (float64(x)+(float64(sx)+0.5)/float64(samples))*step
					fy := offY + (float64(y)+(float64(sy)+0.5)/float64(samples))*step
					c := src.At(b.Min.X+int(fx), b.Min.Y+int(fy))
					cr, cg, cb, _ := c.RGBA()
					r += float64(cr >> 8)
					g += float64(cg >> 8)
					bl += float64(cb >> 8)
					n++
				}
			}
			i := out.PixOffset(x, y)
			out.Pix[i] = uint8(r / n * bright)
			out.Pix[i+1] = uint8(g / n * bright)
			out.Pix[i+2] = uint8(bl / n * bright)
			out.Pix[i+3] = 255
		}
	}
	dir := filepath.Join(miniDir(), "bg")
	os.MkdirAll(dir, 0o755)
	sum := sha1.Sum([]byte(key))
	path := filepath.Join(dir, fmt.Sprintf("%x.png", sum[:6]))
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, buf.Bytes(), 0o644)
}

// update returns the escape sequences needed to show (or remove) the picture.
func (s *sharpLayer) update(img *imageLayer, path string, bright float64, cols, rows int) string {
	if path == "" {
		return s.clear()
	}
	pw, ph := termPixels(cols, rows)
	key := fmt.Sprintf("%s|%.3f|%dx%d|%dx%d", path, bright, cols, rows, pw, ph)
	if key == s.key && s.shown {
		return ""
	}
	if path != img.path || img.src == nil {
		img.path = path
		img.src, img.err = decodeImage(path)
	}
	if img.err != nil {
		s.errMsg = img.err.Error()
		return s.clear()
	}
	file, err := renderSharpFile(img.src, pw, ph, bright, key)
	if err != nil {
		s.errMsg = err.Error()
		return s.clear()
	}
	var sb strings.Builder
	sb.WriteString(s.clear())
	if s.file != "" && s.file != file {
		os.Remove(s.file)
	}
	s.key, s.file, s.shown = key, file, true
	payload := base64.StdEncoding.EncodeToString([]byte(file))
	switch s.kind {
	case sharpKitty:
		// a=T transmit and show, t=f read from file, C=1 keep the cursor,
		// z below -2^30 puts it under text and under cells with a background color
		sb.WriteString("\x1b[H")
		fmt.Fprintf(&sb, "\x1b_Ga=T,f=100,t=f,i=%d,p=1,c=%d,r=%d,C=1,z=-1073741825,q=2;%s\x1b\\",
			kittyImageID, cols, rows, payload)
	case sharpITerm:
		fmt.Fprintf(&sb, "\x1b]1337;SetBackgroundImageFile=%s\x07", payload)
	}
	return sb.String()
}

// clear removes the picture from the terminal.
func (s *sharpLayer) clear() string {
	if !s.shown {
		return ""
	}
	s.shown, s.key = false, ""
	switch s.kind {
	case sharpKitty:
		return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", kittyImageID)
	case sharpITerm:
		return "\x1b]1337;SetBackgroundImageFile=\x07"
	}
	return ""
}

// cleanup is called on quit.
func (s *sharpLayer) cleanup() {
	os.Stdout.WriteString(s.clear())
	if s.file != "" {
		os.Remove(s.file)
	}
}
