package config

import (
	"os"
	"path/filepath"
	"testing"
)

// the module's [[llm.choices]], and a model missing from them goes first
func TestLoadChoices(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(`
[llm]
model = "jarvis"
[[llm.choices]]
model = "jarvis"
name = "Gemma 4 12B"
[[llm.choices]]
model = "jarvis-qwen-4b"
name = "Qwen3.5 4B"
`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.LLM.Choices) != 2 || c.LLM.Choices[1].Name != "Qwen3.5 4B" || c.Character.LoreBudget != 2000 {
		t.Fatalf("%+v", c.LLM)
	}
	os.WriteFile(p, []byte("[llm]\nmodel = \"other\"\n"), 0o644)
	if c, err = Load(p); err != nil || len(c.LLM.Choices) != 1 || c.LLM.Choices[0].Model != "other" {
		t.Fatalf("%+v %v", c.LLM, err)
	}
}
