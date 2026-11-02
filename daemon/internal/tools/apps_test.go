package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func desktop(t *testing.T, dir, id, body string) {
	t.Helper()
	p := filepath.Join(dir, id+".desktop")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testApps(t *testing.T) []App {
	user, sys := t.TempDir(), t.TempDir()
	desktop(t, sys, "steam", "[Desktop Entry]\nType=Application\nName=Steam\nExec=steam %U\n")
	desktop(t, sys, "obsidian", "[Desktop Entry]\nType=Application\nName=Obsidian\nExec=obsidian %u\n")
	desktop(t, sys, "librewolf", "[Desktop Entry]\nType=Application\nName=LibreWolf\nGenericName=Web Browser\nExec=librewolf --name librewolf %U\n[Desktop Action new-window]\nName=Open a New Window\nExec=librewolf --new-window %U\n")
	desktop(t, sys, "org.telegram.desktop", "[Desktop Entry]\nType=Application\nName=Telegram Desktop\nName[ru]=Телеграм\nExec=telegram-desktop -- %u\n")
	desktop(t, sys, "discord", "[Desktop Entry]\nType=Application\nName=Discord\nExec=Discord\n")
	desktop(t, sys, "btop", "[Desktop Entry]\nType=Application\nName=btop++\nExec=btop\nTerminal=true\n")
	desktop(t, sys, "hidden", "[Desktop Entry]\nType=Application\nName=Hidden\nExec=x\nNoDisplay=true\n")
	desktop(t, sys, "link", "[Desktop Entry]\nType=Link\nName=Link\nURL=x\n")
	// the user's own steam wins over the system's
	desktop(t, user, "steam", "[Desktop Entry]\nType=Application\nName=Steam\nExec=steam -silent %U\n")
	return LoadApps([]string{user, sys})
}

func TestLoadApps(t *testing.T) {
	apps := testApps(t)
	if len(apps) != 6 {
		t.Fatalf("got %d apps", len(apps))
	}
	for _, a := range apps {
		if a.ID == "steam" && a.Exec != "steam -silent %U" {
			t.Fatal("the user's entry should win")
		}
		if a.ID == "librewolf" && a.Exec != "librewolf --name librewolf %U" {
			t.Fatalf("an action's Exec leaked in: %q", a.Exec)
		}
	}
}

// spoken names, the way the speech to text writes them
func TestFindApp(t *testing.T) {
	apps := testApps(t)
	for said, want := range map[string]string{
		"обсидиан":    "Obsidian",
		"Стим":        "Steam",
		"steam":       "Steam",
		"либревульф":  "LibreWolf",
		"браузер":     "",
		"web browser": "LibreWolf",
		"телеграм":    "Telegram Desktop",
		"Telegram":    "Telegram Desktop",
		"дискорд":     "Discord",
		"фотошоп":     "",
	} {
		a, near := FindApp(apps, said)
		got := ""
		if a != nil {
			got = a.Name
		}
		if got != want {
			t.Errorf("%q: got %q (near %v), want %q", said, got, near, want)
		}
		if a == nil && len(near) == 0 {
			t.Errorf("%q: no closest ones", said)
		}
	}
}

func TestLaunchArgs(t *testing.T) {
	apps := testApps(t)
	byID := map[string]*App{}
	for i := range apps {
		byID[apps[i].ID] = &apps[i]
	}
	got := strings.Join(LaunchArgs(byID["org.telegram.desktop"], "kitty"), " ")
	if !strings.HasPrefix(got, "systemd-run --user --collect --quiet --unit app-jarvis-org_telegram_desktop-") || !strings.HasSuffix(got, " -- /bin/sh -c telegram-desktop --") {
		t.Fatalf("got %q", got)
	}
	got = strings.Join(LaunchArgs(byID["btop"], "kitty"), " ")
	if !strings.HasSuffix(got, "/bin/sh -c kitty -e btop") {
		t.Fatalf("terminal app: %q", got)
	}
}
