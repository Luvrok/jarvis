package daemon

import (
	"os"
	"strings"
	"testing"

	"jarvis/internal/config"
	"jarvis/internal/llm"
)

func testDaemon(budget int) *Daemon {
	cfg := &config.Config{}
	cfg.Character.LoreBudget = budget
	return &Daemon{cfg: cfg, basePrompt: "You are {{name}}. {{lang_rule}}"}
}

func entry(t *testing.T, key, content string) config.LoreEntry {
	t.Helper()
	p := write(t, `{"entries": [{"keys": ["`+key+`"], "content": "`+content+`"}]}`)
	lore, err := config.LoadLorebook(p)
	if err != nil || len(lore) != 1 {
		t.Fatalf("%v %v", lore, err)
	}
	return lore[0]
}

// the system prompt has nothing that changes between questions, the time
// and the lore are in the last message only
func TestPromptStaysTheSame(t *testing.T) {
	d := testDaemon(1000)
	ch := &config.Character{Name: "Kurisu", Lang: "auto", Persona: "persona"}
	ch.Lore = []config.LoreEntry{entry(t, "SERN", "SERN is the enemy.")}
	sys := d.systemPrompt(ch)
	if strings.Contains(sys, "{{") || strings.Contains(sys, "Now:") {
		t.Fatalf("system prompt: %q", sys)
	}
	msg := d.withContext(ch, "what is sern", nil)
	if !strings.Contains(msg, "Now: ") || !strings.Contains(msg, "SERN is the enemy.") || !strings.HasSuffix(msg, "what is sern") {
		t.Fatalf("message: %q", msg)
	}
	// the last exchange counts: "and them?" after an answer about sern
	prev := []llm.Message{{Role: "system", Content: "SERN"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "SERN again"}}
	if !strings.Contains(d.withContext(ch, "and them?", prev), "SERN is the enemy.") {
		t.Fatal("the last exchange should bring the entry")
	}
	if strings.Contains(d.withContext(ch, "hello", prev[:2]), "SERN is the enemy.") {
		t.Fatal("the system prompt mustn't count")
	}
}

// entries over the budget are left out, smaller later ones still fit
func TestLoreBudget(t *testing.T) {
	d := testDaemon(20)
	ch := &config.Character{Lore: []config.LoreEntry{
		entry(t, "a", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		entry(t, "b", "bbbbbbbbbb"),
	}}
	got := d.lore(ch, "a b", nil)
	if len(got) != 1 || got[0] != "bbbbbbbbbb" {
		t.Fatalf("got %q", got)
	}
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := t.TempDir() + "/lorebook.json"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
