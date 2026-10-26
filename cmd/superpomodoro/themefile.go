package main

// Theme files: ~/.config/superpomodoro/themes/<name>.theme
//
//   extends = "nord"                  # start from any built-in or custom theme
//   focus   = "#bf616a #d08770"       # gradient pair (one color is fine too)
//   break   = "#88c0d0 #a3be8c"
//   long    = "#81a1c1 #b48ead"
//   text    = "#eceff4"
//   muted   = "#4c566a"
//   surface = "#3b4252"
//   scene   = "waves"                 # rain, waves, grid or off
//   image   = "~/Pictures/fjord.jpg"  # optional background image
//   image_brightness = 0.45
//
// Every key is optional. Files are reloaded live while the timer runs.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func themeDir() string { return filepath.Join(filepath.Dir(configPath()), "themes") }

var hexColor = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

func parseColors(v string) ([]rgb, error) {
	var out []rgb
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == ',' }) {
		if !hexColor.MatchString(f) {
			return nil, fmt.Errorf("%q is not a #rrggbb color", f)
		}
		out = append(out, hex(f))
	}
	if len(out) == 0 || len(out) > 2 {
		return nil, fmt.Errorf("expected one or two colors")
	}
	if len(out) == 1 {
		out = append(out, out[0])
	}
	return out, nil
}

func findTheme(list []Theme, name string) (Theme, bool) {
	for _, t := range list {
		if t.Name == name {
			return t, true
		}
	}
	return Theme{}, false
}

// parseThemeFile reads one theme file on top of the themes known so far.
func parseThemeFile(path string, known []Theme) (Theme, error) {
	name := strings.TrimSuffix(filepath.Base(path), ".theme")
	f, err := os.Open(path)
	if err != nil {
		return Theme{}, err
	}
	defer f.Close()

	kv := map[string]string{}
	var order []string
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "#"); i == 0 {
			continue
		}
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return Theme{}, fmt.Errorf("%s line %d: expected key = value", name, n)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// allow trailing comments after a quoted value
		if strings.HasPrefix(v, `"`) {
			if end := strings.Index(v[1:], `"`); end >= 0 {
				v = v[1 : end+1]
			}
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		kv[k] = v
		order = append(order, k)
	}

	t := builtinThemes[0]
	if base, ok := kv["extends"]; ok {
		bt, found := findTheme(known, base)
		if !found {
			return Theme{}, fmt.Errorf("%s: unknown theme %q in extends", name, base)
		}
		t = bt
		t.Extends = base
	} else if bt, found := findTheme(known, name); found {
		t = bt // a file named like a built-in theme modifies that theme
	}
	t.Name, t.Custom, t.Base = name, true, rgb{}

	for _, k := range order {
		v := kv[k]
		var err error
		switch k {
		case "extends":
		case "name":
			t.Name = v
		case "focus", "break", "long":
			var cs []rgb
			if cs, err = parseColors(v); err == nil {
				switch k {
				case "focus":
					t.FocusA, t.FocusB = cs[0], cs[1]
				case "break":
					t.BreakA, t.BreakB = cs[0], cs[1]
				default:
					t.LongA, t.LongB = cs[0], cs[1]
				}
			}
		case "text", "muted", "surface":
			var cs []rgb
			if cs, err = parseColors(v); err == nil {
				switch k {
				case "text":
					t.Text = cs[0]
				case "muted":
					t.Muted = cs[0]
				default:
					t.Surface = cs[0]
				}
			}
		case "scene":
			if v != "rain" && v != "waves" && v != "grid" && v != "off" {
				err = fmt.Errorf("scene must be rain, waves, grid or off")
			}
			t.Scene = v
		case "author":
			t.Author = v
		case "description":
			t.Description = v
		case "image":
			t.Image = v
		case "image_brightness":
			t.ImageBrightness, err = strconv.ParseFloat(v, 64)
		case "image_fit":
			if v != "auto" && v != "cover" && v != "contain" {
				err = fmt.Errorf("image_fit must be auto, cover or contain")
			}
			t.ImageFit = v
		case "image_scale":
			t.ImageScale, err = strconv.ParseFloat(v, 64)
			if err == nil && (t.ImageScale <= 0 || t.ImageScale > 1) {
				err = fmt.Errorf("image_scale must be between 0 and 1")
			}
		case "image_position":
			if v != "top" && v != "center" && v != "bottom" {
				err = fmt.Errorf("image_position must be top, center or bottom")
			}
			t.ImagePosition = v
		default:
			err = fmt.Errorf("unknown key %q", k)
		}
		if err != nil {
			return Theme{}, fmt.Errorf("%s: %s: %v", name, k, err)
		}
	}
	return t, nil
}

// loadThemes rebuilds the theme list from the built-ins plus every theme file.
// Errors don't stop loading, the first one is returned for display.
func loadThemes() ([]Theme, error) {
	list := withScenes(builtinThemes)
	files, _ := filepath.Glob(filepath.Join(themeDir(), "*.theme"))
	sort.Strings(files)
	var firstErr error
	for _, f := range files {
		t, err := parseThemeFile(f, list)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		replaced := false
		for i := range list {
			if list[i].Name == t.Name {
				list[i], replaced = t, true
			}
		}
		if !replaced {
			list = append(list, t)
		}
	}
	return list, firstErr
}

// themeFilesStamp changes whenever a theme file is added, removed or edited.
func themeFilesStamp() string {
	files, _ := filepath.Glob(filepath.Join(themeDir(), "*.theme"))
	var sb strings.Builder
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(&sb, "%s:%d:%d;", f, st.ModTime().UnixNano(), st.Size())
		}
	}
	return sb.String()
}

// reloadThemesIfChanged is called from the main loop about once a second.
func (a *App) reloadThemesIfChanged() {
	if time.Since(a.themeCheck) < time.Second {
		return
	}
	a.themeCheck = time.Now()
	stamp := themeFilesStamp()
	if stamp == a.themeStamp {
		return
	}
	a.themeStamp = stamp
	current := themes[a.theme].Name
	list, err := loadThemes()
	themes = list
	a.theme = themeIndex(current)
	a.thCache = nil
	if err != nil {
		a.setFlash("theme file: " + err.Error())
	} else {
		a.setFlash("themes reloaded")
	}
}

// themeCommand implements `superpomodoro theme list|new|dir`.
func themeCommand(args []string) int {
	list, err := loadThemes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list", "":
		for _, t := range list {
			kind := "built-in"
			if t.Custom {
				kind = "custom"
			}
			fmt.Printf("%-14s %-9s scene %s\n", t.Name, kind, t.Scene)
		}
		fmt.Println("\ntheme files:", themeDir())
	case "dir":
		fmt.Println(themeDir())
	case "gen", "edit", "editor":
		return themegenCommand(args[1:])
	case "export":
		return exportCommand(args[1:])
	case "import":
		return importCommand(args[1:])
	case "new":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: superpomodoro theme new <name> [--from <theme>]")
			return 2
		}
		name, from := args[1], "tokyo-night"
		if len(args) >= 4 && args[2] == "--from" {
			from = args[3]
		}
		base, ok := findTheme(list, from)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown theme %q\n", from)
			return 2
		}
		path := filepath.Join(themeDir(), name+".theme")
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(os.Stderr, "%s already exists\n", path)
			return 1
		}
		os.MkdirAll(themeDir(), 0o755)
		content := fmt.Sprintf(`# SuperPomodoro theme %q, based on %s
# Every line is optional. Save the file and a running timer picks it up live.

extends = %q

# gradient pairs for the digits and the progress bar
focus   = "%s %s"
break   = "%s %s"
long    = "%s %s"

# interface text, secondary text, subtle decoration
text    = %q
muted   = %q
surface = %q

# animated scene: rain, waves, grid or off
scene   = %q

# background image (png, jpg or gif) and its brightness from 0 to 1
# image = "~/Pictures/wallpaper.jpg"
# image_brightness = 0.45
`, name, from, from,
			base.FocusA.hexStr(), base.FocusB.hexStr(), base.BreakA.hexStr(), base.BreakB.hexStr(),
			base.LongA.hexStr(), base.LongB.hexStr(), base.Text.hexStr(), base.Muted.hexStr(), base.Surface.hexStr(), base.Scene)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("created", path)
		fmt.Printf("start it with: superpomodoro --theme %s\n", name)
	default:
		fmt.Fprintln(os.Stderr, "usage: superpomodoro theme [list|new|edit|export|import|dir]")
		return 2
	}
	return 0
}
