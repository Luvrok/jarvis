package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lorebook.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// sillytavern: entries by uid, key, keysecondary, disable, order
func TestLorebookSillyTavern(t *testing.T) {
	p := write(t, `{"entries": {
		"0": {"key": ["D-mail", "ди-мейл"], "content": "A text message sent to the past.", "order": 20},
		"1": {"key": ["SERN"], "content": "The organization.", "order": 10},
		"2": {"key": ["off"], "content": "disabled", "disable": true},
		"3": {"key": [], "content": "Always there.", "constant": true, "order": 99},
		"4": {"key": ["Okabe"], "keysecondary": ["lab"], "selective": true, "content": "Needs both."}
	}}`)
	lore, err := LoadLorebook(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(lore) != 4 {
		t.Fatalf("got %d entries, want 4", len(lore))
	}
	// constant first, then by order
	if !lore[0].Constant || lore[1].Content != "The organization." || lore[2].Order != 20 {
		t.Fatalf("order: %+v", lore)
	}
	cases := []struct {
		text string
		want []string
	}{
		{"что такое ДИ-МЕЙЛ?", []string{"Always there.", "A text message sent to the past."}},
		{"tell me about sern", []string{"Always there.", "The organization."}},
		// inside a word doesn't count
		{"concerned", []string{"Always there."}},
		{"where is Okabe", []string{"Always there."}},
		{"is Okabe in the lab", []string{"Always there.", "Needs both."}},
	}
	for _, c := range cases {
		var got []string
		for _, e := range lore {
			if e.Matches(c.text) {
				got = append(got, e.Content)
			}
		}
		if len(got) != len(c.want) {
			t.Fatalf("%q: got %q, want %q", c.text, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%q: got %q, want %q", c.text, got, c.want)
			}
		}
	}
}

// chub / character book v2: a list, keys, enabled, insertion_order, a
// russian key matches with an ending, a /regex/ key
func TestLorebookChub(t *testing.T) {
	p := write(t, `{"name": "x", "entries": [
		{"keys": ["машин"], "content": "Time machine.", "enabled": true, "insertion_order": 1},
		{"keys": ["/kurisu|курису/i"], "content": "Kurisu.", "insertion_order": 2},
		{"keys": ["gone"], "content": "off", "enabled": false}
	]}`)
	lore, err := LoadLorebook(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(lore) != 2 {
		t.Fatalf("got %d entries, want 2", len(lore))
	}
	if !lore[0].Matches("построить машину времени") {
		t.Fatal("машин should find машину")
	}
	if !lore[1].Matches("а где КУРИСУ") || lore[1].Matches("hi") {
		t.Fatal("the regex key")
	}
}

// a character card with the book inside, and no file at all
func TestLorebookCardAndMissing(t *testing.T) {
	p := write(t, `{"data": {"character_book": {"entries": [{"keys": ["a"], "content": "A."}]}}}`)
	lore, err := LoadLorebook(p)
	if err != nil || len(lore) != 1 {
		t.Fatalf("card: %v %v", lore, err)
	}
	lore, err = LoadLorebook(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || lore != nil {
		t.Fatalf("missing: %v %v", lore, err)
	}
}
