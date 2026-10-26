package main

// Theme files: ~/.config/superpomodoro/themes/<name>.theme
//
//   extends = "dracula"               # start from any built-in or custom theme
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
	"errors"
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
		case "image_shift_x", "image_shift_y":
			var f float64
			f, err = strconv.ParseFloat(v, 64)
			if err == nil && (f < -1 || f > 1) {
				err = fmt.Errorf("%s must be between -1 and 1", k)
			}
			if k == "image_shift_x" {
				t.ImageShiftX = f
			} else {
				t.ImageShiftY = f
			}
		default:
			err = fmt.Errorf("unknown key %q", k)
		}
		if err != nil {
			return Theme{}, fmt.Errorf("%s: %s: %v", name, k, err)
		}
	}
	return t, nil
}

// loadThemes returns the themes the user can pick: everything from loadAllThemes
// minus the ones they removed. Hidden themes still work as a base for extends.
func loadThemes() ([]Theme, error) {
	all, err := loadAllThemes()
	hidden := hiddenThemes()
	var list []Theme
	for _, t := range all {
		if !hidden[t.Name] {
			list = append(list, t)
		}
	}
	if len(list) == 0 { // never leave the app without a theme
		return all, err
	}
	return list, err
}

// hiddenThemes reads the removed built-in themes from the config.
func hiddenThemes() map[string]bool {
	out := map[string]bool{}
	for _, n := range strings.Split(loadConfig().HiddenThemes, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out[n] = true
		}
	}
	return out
}

// setThemeHidden removes a theme from the picker or brings it back.
func setThemeHidden(name string, hide bool) {
	h := hiddenThemes()
	if hide {
		h[name] = true
	} else {
		delete(h, name)
	}
	names := make([]string, 0, len(h))
	for n := range h {
		names = append(names, n)
	}
	sort.Strings(names)
	c := loadConfig()
	c.HiddenThemes = strings.Join(names, ",")
	saveConfig(c)
}

// removeTheme deletes a custom theme file. A built-in theme (or a built-in that
// is left after deleting the file that changed it) is hidden only when hideBuiltin is set.
// It reports what happened in words.
func removeTheme(name string, hideBuiltin bool) (string, error) {
	if !themeNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid theme name %q", name)
	}
	path := filepath.Join(themeDir(), name+".theme")
	_, isBuiltin := findTheme(builtinThemes, name)
	if err := os.Remove(path); err == nil {
		if isBuiltin && !hideBuiltin {
			return "deleted your changes, the original " + name + " is back", nil
		}
		if !isBuiltin {
			return "deleted " + name, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	} else if !isBuiltin {
		return "", fmt.Errorf("no theme named %q", name)
	}
	setThemeHidden(name, true)
	return "removed " + name + ", bring it back with: superpomodoro theme restore " + name, nil
}

// loadAllThemes rebuilds the theme list from the built-ins plus every theme file.
// Errors don't stop loading, the first one is returned for display.
func loadAllThemes() ([]Theme, error) {
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
	sb.WriteString(loadConfig().HiddenThemes + ";")
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
		all, _ := loadAllThemes()
		hidden := hiddenThemes()
		for _, t := range all {
			kind := "built-in"
			if t.Custom {
				kind = "custom"
			}
			if hidden[t.Name] {
				kind = "removed"
			}
			fmt.Printf("%-14s %-9s scene %s\n", t.Name, kind, t.Scene)
		}
		fmt.Println("\ntheme files:", themeDir())
	case "rm", "remove", "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: superpomodoro theme rm <name> [--all]")
			return 2
		}
		// --all also removes the built-in when deleting a file that changed one
		all := len(args) > 2 && args[2] == "--all"
		msg, err := removeTheme(args[1], all)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(msg)
	case "restore":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: superpomodoro theme restore <name|--all>")
			return 2
		}
		if args[1] == "--all" {
			c := loadConfig()
			c.HiddenThemes = ""
			saveConfig(c)
			fmt.Println("all built-in themes are back")
			return 0
		}
		if !hiddenThemes()[args[1]] {
			fmt.Fprintf(os.Stderr, "%s is not removed\n", args[1])
			return 1
		}
		setThemeHidden(args[1], false)
		fmt.Println("restored", args[1])
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
		fmt.Fprintln(os.Stderr, "usage: superpomodoro theme [list|new|edit|rm|restore|export|import|dir]")
		return 2
	}
	return 0
}
