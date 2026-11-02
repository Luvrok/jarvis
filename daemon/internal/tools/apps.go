package tools

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// App: a desktop entry that can be launched
type App struct {
	ID       string
	Name     string
	Exec     string
	Terminal bool
	// what the user may call it: the names, the generic name, the
	// keywords, the file name, in latin lowercase letters and digits
	words []string
}

// AppDirs: the applications folders, XDG_DATA_HOME and XDG_DATA_DIRS, and
// nixos' profiles when those aren't in the service's environment
func AppDirs() []string {
	home, _ := os.UserHomeDir()
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local/share")
	}
	dirs := []string{data}
	for _, d := range filepath.SplitList(os.Getenv("XDG_DATA_DIRS")) {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	dirs = append(dirs,
		filepath.Join(home, ".nix-profile/share"),
		"/etc/profiles/per-user/"+os.Getenv("USER")+"/share",
		"/run/current-system/sw/share",
	)
	var out []string
	seen := map[string]bool{}
	for _, d := range dirs {
		d = filepath.Join(d, "applications")
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// LoadApps: the launchable entries of dirs, the first one of an id wins,
// the way the spec has it
func LoadApps(dirs []string) []App {
	var out []App
	seen := map[string]bool{}
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			id := strings.ReplaceAll(strings.TrimSuffix(rel, ".desktop"), "/", "-")
			if seen[id] {
				return nil
			}
			seen[id] = true
			if a, ok := parseDesktop(path, id); ok {
				out = append(out, a)
			}
			return nil
		})
	}
	return out
}

func parseDesktop(path, id string) (App, bool) {
	f, err := os.Open(path)
	if err != nil {
		return App{}, false
	}
	defer f.Close()
	a := App{ID: id}
	var names []string
	typ, hidden := "", false
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			in = line == "[Desktop Entry]"
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !in || !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case k == "Type":
			typ = v
		case k == "Name":
			a.Name = v
			names = append(names, v)
		case k == "Exec":
			a.Exec = v
		case k == "Terminal":
			a.Terminal = v == "true"
		case k == "NoDisplay" || k == "Hidden":
			hidden = hidden || v == "true"
		case strings.HasPrefix(k, "Name[") || strings.HasPrefix(k, "GenericName"):
			names = append(names, v)
		case strings.HasPrefix(k, "Keywords"):
			names = append(names, strings.Split(v, ";")...)
		}
	}
	if typ != "Application" || hidden || a.Exec == "" || a.Name == "" {
		return App{}, false
	}
	names = append(names, filepath.Base(id))
	// a reverse dns id: its last part (org.kde.dolphin -> dolphin)
	if i := strings.LastIndex(id, "."); i >= 0 {
		names = append(names, id[i+1:])
	}
	for _, n := range names {
		if w := norm(n); w != "" {
			a.words = append(a.words, w)
		}
		// each word of a longer name too: "Telegram Desktop" is "telegram"
		for _, part := range strings.Fields(n) {
			if w := norm(part); len(w) > 2 {
				a.words = append(a.words, w)
			}
		}
	}
	return a, true
}

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "c",
	'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya",
}

// norm: latin lowercase letters and digits, cyrillic spelled in latin, so
// "Обсидиан" from the speech and "Obsidian" from the entry meet
func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if t, ok := translit[r]; ok {
			b.WriteString(t)
		} else if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// score: how well a spoken name fits one of the app's words, 0..1
func score(q, w string) float64 {
	switch {
	case q == w:
		return 1
	case strings.HasPrefix(w, q) || strings.HasPrefix(q, w):
		return 0.9
	case strings.Contains(w, q) || strings.Contains(q, w):
		return 0.8
	}
	// a word heard a bit wrong: "stim" is steam, "librevulf" librewolf
	n := max(len(q), len(w))
	return 1 - float64(levenshtein(q, w))/float64(n)
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// the least a spoken name has to fit
const minScore = 0.6

// FindApp: the app a spoken name means, and the closest ones when none fits
func FindApp(apps []App, name string) (*App, []string) {
	q := norm(name)
	if q == "" {
		return nil, nil
	}
	type hit struct {
		app   *App
		score float64
	}
	var hits []hit
	for i := range apps {
		best := 0.0
		for _, w := range apps[i].words {
			best = max(best, score(q, w))
		}
		hits = append(hits, hit{&apps[i], best})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		// the plainer one: "Steam" before "Steam (Runtime)"
		return len(hits[i].app.Name) < len(hits[j].app.Name)
	})
	if len(hits) > 0 && hits[0].score >= minScore {
		return hits[0].app, nil
	}
	var near []string
	for _, h := range hits[:min(3, len(hits))] {
		near = append(near, h.app.Name)
	}
	return nil, near
}

// field codes the spec puts in Exec for files and urls, nothing to give them
var fieldCode = regexp.MustCompile(`\s*%[fFuUdDnNickvm]`)

// LaunchArgs: the command that starts the app as its own systemd user
// unit, so it lives on when jarvis restarts
func LaunchArgs(a *App, terminal string) []string {
	cmd := strings.ReplaceAll(fieldCode.ReplaceAllString(a.Exec, ""), "%%", "%")
	if a.Terminal && terminal != "" {
		cmd = terminal + " -e " + cmd
	}
	unit := fmt.Sprintf("app-jarvis-%s-%d", strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, a.ID), time.Now().UnixNano()%1e6)
	return []string{"systemd-run", "--user", "--collect", "--quiet", "--unit", unit, "--", "/bin/sh", "-c", cmd}
}

// openApp: finds and starts the app, the text says what happened
func (s *Set) openApp(name string) (string, error) {
	apps := LoadApps(AppDirs())
	a, near := FindApp(apps, name)
	if a == nil {
		if len(near) == 0 {
			return "", fmt.Errorf("no apps found on this computer")
		}
		return fmt.Sprintf("no app called %q. the closest: %s", name, strings.Join(near, ", ")), nil
	}
	args := LaunchArgs(a, s.terminal)
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s: %v %s", a.Name, err, strings.TrimSpace(string(out)))
	}
	return "opened " + a.Name, nil
}
