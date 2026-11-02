package config

import (
	"os"
	"path/filepath"
	"testing"
)

// [[stt.choices]] and the default, and an old config with only stt.model
func TestLoadSTT(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(`
[stt]
default = "gigaam-v2"
[[stt.choices]]
id = "parakeet-v3"
name = "Parakeet v3"
kind = "transducer"
dir = "/m/parakeet"
[[stt.choices]]
id = "gigaam-v2"
name = "GigaAM v2"
kind = "transducer"
dir = "/m/gigaam"
`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.STT.Choices) != 2 || c.STT.Default != "gigaam-v2" || c.STT.Choices[1].Dir != "/m/gigaam" {
		t.Fatalf("%+v", c.STT)
	}
	os.WriteFile(p, []byte("[stt]\nmodel = \"/m/whisper\"\n"), 0o644)
	if c, err = Load(p); err != nil || len(c.STT.Choices) != 1 || c.STT.Default != "whisper" || c.STT.Choices[0].Kind != "whisper" {
		t.Fatalf("%+v %v", c.STT, err)
	}
}
