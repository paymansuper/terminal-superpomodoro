package main

// The .spomotheme exchange format (see docs/theme-format.md).
//
// Local themes are stored as small .theme text files. To share them, they are
// packed into one self contained JSON document: colors fully resolved (so the
// receiver doesn't need the base theme) and the background image embedded as
// base64. One file can carry one theme or a whole collection.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	themeFormatID      = "superpomodoro-theme"
	themeFormatVersion = 1
	themeFileExt       = ".spomotheme"
	maxImageBytes      = 20 << 20
)

type themePackage struct {
	Format    string          `json:"format"`
	Version   int             `json:"version"`
	Generator string          `json:"generator,omitempty"`
	Exported  string          `json:"exported,omitempty"`
	Themes    []portableTheme `json:"themes"`
}

type portableTheme struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Author      string         `json:"author,omitempty"`
	Extends     string         `json:"extends,omitempty"`
	Colors      portableColors `json:"colors"`
	Scene       string         `json:"scene"`
	Image       *portableImage `json:"image,omitempty"`
}

type portableColors struct {
	Focus   [2]string `json:"focus"`
	Break   [2]string `json:"break"`
	Long    [2]string `json:"long"`
	Text    string    `json:"text"`
	Muted   string    `json:"muted"`
	Surface string    `json:"surface"`
}

type portableImage struct {
	File       string  `json:"file"`
	Brightness float64 `json:"brightness,omitempty"`
	Fit        string  `json:"fit,omitempty"`      // auto, cover, contain
	Scale      float64 `json:"scale,omitempty"`    // contain size, 0..1
	Position   string  `json:"position,omitempty"` // top, center, bottom
	Data       string  `json:"data"`               // base64 of the original file
}

var unsafeNameChars = regexp.MustCompile(`[^a-z0-9-]+`)

// sanitizeName turns anything into a valid theme or file name.
func sanitizeName(s, fallback string) string {
	s = strings.Trim(unsafeNameChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
	if len(s) > 32 {
		s = strings.Trim(s[:32], "-")
	}
	if s == "" {
		return fallback
	}
	return s
}

// ---------- export ----------

func portableFromJSON(j themeJSON) (portableTheme, error) {
	p := portableTheme{
		Name: j.Name, Description: j.Description, Author: j.Author, Extends: j.Extends,
		Colors: portableColors{Focus: j.Focus, Break: j.Break, Long: j.Long, Text: j.Text, Muted: j.Muted, Surface: j.Surface},
		Scene:  j.Scene,
	}
	if j.Image != "" {
		data, err := os.ReadFile(expandHome(j.Image))
		if err != nil {
			return p, fmt.Errorf("%s: image %s can't be read", j.Name, filepath.Base(j.Image))
		}
		if len(data) > maxImageBytes {
			return p, fmt.Errorf("%s: image is larger than 20 MB", j.Name)
		}
		p.Image = &portableImage{File: filepath.Base(j.Image), Brightness: j.ImageBrightness,
			Fit: j.ImageFit, Scale: j.ImageScale, Position: j.ImagePosition,
			Data: base64.StdEncoding.EncodeToString(data)}
	}
	return p, nil
}

func buildPackage(list []themeJSON) (themePackage, error) {
	pkg := themePackage{Format: themeFormatID, Version: themeFormatVersion,
		Generator: "superpomodoro " + version, Exported: time.Now().UTC().Format(time.RFC3339)}
	for _, j := range list {
		p, err := portableFromJSON(j)
		if err != nil {
			return pkg, err
		}
		pkg.Themes = append(pkg.Themes, p)
	}
	if len(pkg.Themes) == 0 {
		return pkg, errors.New("nothing to export")
	}
	return pkg, nil
}

func marshalPackage(pkg themePackage) ([]byte, error) {
	return json.MarshalIndent(pkg, "", "  ")
}

// ---------- import ----------

type importResult struct {
	Imported []string          `json:"imported"`
	Renamed  map[string]string `json:"renamed,omitempty"` // original name -> stored name
}

func themeFileExists(name string) bool {
	_, err := os.Stat(filepath.Join(themeDir(), name+".theme"))
	return err == nil
}

// freeName finds a name that doesn't clash with an existing theme.
func freeName(name string, known []Theme, taken map[string]bool) string {
	clash := func(n string) bool {
		_, ok := findTheme(known, n)
		return ok || taken[n] || themeFileExists(n)
	}
	if !clash(name) {
		return name
	}
	for i := 2; ; i++ {
		n := fmt.Sprintf("%s-%d", strings.TrimSuffix(name[:min(len(name), 29)], "-"), i)
		if !clash(n) {
			return n
		}
	}
}

func saveImportedImage(img *portableImage, themeName string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return "", errors.New("embedded image is not valid base64")
	}
	if len(data) > maxImageBytes {
		return "", errors.New("embedded image is larger than 20 MB")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return "", errors.New("embedded image is not a png, jpg or gif")
	}
	ext := strings.ToLower(filepath.Ext(img.File))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		ext = ".png"
	}
	base := sanitizeName(strings.TrimSuffix(filepath.Base(img.File), filepath.Ext(img.File)), themeName)
	os.MkdirAll(themeImagesDir(), 0o755)
	dst := filepath.Join(themeImagesDir(), base+ext)
	for i := 2; ; i++ {
		existing, err := os.ReadFile(dst)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err == nil && bytes.Equal(existing, data) {
			return dst, nil // same picture already there
		}
		dst = filepath.Join(themeImagesDir(), fmt.Sprintf("%s-%d%s", base, i, ext))
	}
	return dst, os.WriteFile(dst, data, 0o644)
}

// importThemes installs every theme of a package or a single .theme file.
// Without overwrite, clashing names get a numeric suffix.
func importThemes(data []byte, filename string, overwrite bool) (importResult, error) {
	res := importResult{Renamed: map[string]string{}}
	known, _ := loadThemes()

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return res, errors.New("the file is empty")
	}
	if trimmed[0] != '{' {
		return importThemeFile(data, filename, known, overwrite)
	}

	var pkg themePackage
	if err := json.Unmarshal(trimmed, &pkg); err != nil {
		return res, fmt.Errorf("not a valid %s file: %v", themeFileExt, err)
	}
	if pkg.Format != themeFormatID {
		return res, fmt.Errorf("unknown format %q, expected %q", pkg.Format, themeFormatID)
	}
	if pkg.Version < 1 || pkg.Version > themeFormatVersion {
		return res, fmt.Errorf("format version %d is not supported, update SuperPomodoro", pkg.Version)
	}
	if len(pkg.Themes) == 0 {
		return res, errors.New("the file contains no themes")
	}

	// validate everything first, so a broken file doesn't half import
	type prepared struct {
		j     themeJSON
		image *portableImage
		orig  string
	}
	var todo []prepared
	taken := map[string]bool{}
	for i, p := range pkg.Themes {
		orig := p.Name
		name := sanitizeName(p.Name, fmt.Sprintf("imported-%d", i+1))
		if !overwrite {
			name = freeName(name, known, taken)
		}
		taken[name] = true
		j := themeJSON{Name: name, Description: p.Description, Author: p.Author,
			Focus: p.Colors.Focus, Break: p.Colors.Break, Long: p.Colors.Long,
			Text: p.Colors.Text, Muted: p.Colors.Muted, Surface: p.Colors.Surface, Scene: p.Scene}
		if j.Scene == "" {
			j.Scene = "off"
		}
		if _, ok := findTheme(known, p.Extends); ok && p.Extends != name {
			j.Extends = p.Extends // purely informational, colors are complete anyway
		}
		if p.Image != nil {
			j.ImageBrightness = p.Image.Brightness
			j.ImageFit, j.ImageScale, j.ImagePosition = p.Image.Fit, p.Image.Scale, p.Image.Position
			j.Image = "pending" // placeholder for validation
		}
		if _, err := j.fileContent(); err != nil {
			return res, fmt.Errorf("theme %q: %v", orig, err)
		}
		todo = append(todo, prepared{j, p.Image, orig})
	}

	os.MkdirAll(themeDir(), 0o755)
	for _, t := range todo {
		j := t.j
		j.Image = ""
		if t.image != nil {
			path, err := saveImportedImage(t.image, j.Name)
			if err != nil {
				return res, fmt.Errorf("theme %q: %v", t.orig, err)
			}
			j.Image = path
		}
		content, _ := j.fileContent()
		if err := os.WriteFile(filepath.Join(themeDir(), j.Name+".theme"), []byte(content), 0o644); err != nil {
			return res, err
		}
		res.Imported = append(res.Imported, j.Name)
		if j.Name != t.orig {
			res.Renamed[t.orig] = j.Name
		}
	}
	return res, nil
}

// importThemeFile accepts a plain .theme text file as well.
func importThemeFile(data []byte, filename string, known []Theme, overwrite bool) (importResult, error) {
	res := importResult{Renamed: map[string]string{}}
	orig := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	name := sanitizeName(orig, "imported")
	if !overwrite {
		name = freeName(name, known, map[string]bool{})
	}
	tmp, err := os.CreateTemp("", "spomo-*.theme")
	if err != nil {
		return res, err
	}
	defer os.Remove(tmp.Name())
	tmp.Write(data)
	tmp.Close()
	if _, err := parseThemeFile(tmp.Name(), known); err != nil {
		return res, fmt.Errorf("not a valid theme file: %v", err)
	}
	os.MkdirAll(themeDir(), 0o755)
	if err := os.WriteFile(filepath.Join(themeDir(), name+".theme"), data, 0o644); err != nil {
		return res, err
	}
	res.Imported = []string{name}
	if name != orig {
		res.Renamed[orig] = name
	}
	return res, nil
}

// ---------- command line ----------

func exportCommand(args []string) int {
	var names []string
	out, all := "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all", "-a":
			all = true
		case "-o", "--output":
			if i+1 < len(args) {
				out = args[i+1]
				i++
			}
		default:
			names = append(names, args[i])
		}
	}
	list, _ := loadThemes()
	var sel []themeJSON
	for _, t := range list {
		if all && t.Custom {
			sel = append(sel, toJSON(t))
		}
	}
	for _, n := range names {
		t, ok := findTheme(list, n)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown theme %q, see: superpomodoro theme list\n", n)
			return 2
		}
		sel = append(sel, toJSON(t))
	}
	if len(sel) == 0 {
		fmt.Fprintln(os.Stderr, "usage: superpomodoro theme export <name>... [-o file]")
		fmt.Fprintln(os.Stderr, "       superpomodoro theme export --all [-o file]   (all custom themes)")
		return 2
	}
	pkg, err := buildPackage(sel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	data, _ := marshalPackage(pkg)
	if out == "-" {
		os.Stdout.Write(append(data, '\n'))
		return 0
	}
	if out == "" {
		out = sel[0].Name + themeFileExt
		if len(sel) > 1 {
			out = "superpomodoro-themes" + themeFileExt
		}
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("exported %d theme(s) to %s\n", len(sel), out)
	return 0
}

func importCommand(args []string) int {
	overwrite := false
	var files []string
	for _, a := range args {
		if a == "--force" || a == "-f" {
			overwrite = true
		} else {
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: superpomodoro theme import <file"+themeFileExt+"|file.theme>... [--force]")
		return 2
	}
	code := 0
	for _, f := range files {
		data, err := os.ReadFile(expandHome(f))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
			continue
		}
		res, err := importThemes(data, f, overwrite)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			code = 1
			continue
		}
		for _, n := range res.Imported {
			note := ""
			for o, r := range res.Renamed {
				if r == n {
					note = fmt.Sprintf("  (was %q, renamed because the name is taken)", o)
				}
			}
			fmt.Printf("imported %s%s\n", n, note)
		}
	}
	if code == 0 {
		fmt.Println("use it with: superpomodoro --theme <name>")
	}
	return code
}
