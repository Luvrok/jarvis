// config: /etc/jarvis/config.toml from the module, the characters folder,
// and the user's picks in ~/.local/state/jarvis/state.toml
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const DefaultPath = "/etc/jarvis/config.toml"

type Config struct {
	LLM struct {
		URL string `toml:"url"`
		// the one at the start, the menu's pick wins after
		Model     string `toml:"model"`
		MaxTokens int    `toml:"max_tokens"`
		// the llama-swap models the menu offers, Model among them
		Choices []LLMChoice `toml:"choices"`
	} `toml:"llm"`
	TTS struct {
		URL string `toml:"url"`
		// a quicker model for the cloned voices, "" for none
		SmallModel string `toml:"small_model"`
	} `toml:"tts"`
	Character struct {
		Default string `toml:"default"`
		Dir     string `toml:"dir"`
		// prompts/base.md, shared by every character
		BasePrompt string `toml:"base_prompt"`
		// refs/<id>/ for the cloned voices, only checked here
		Refs string `toml:"refs"`
		// the most characters of lorebook entries in one question
		LoreBudget int `toml:"lore_budget"`
	} `toml:"character"`
	STT struct {
		// a whisper folder, the one model before there were choices
		Model   string `toml:"model"`
		Threads int    `toml:"threads"`
		// the one at the start, the menu's pick wins after
		Default string      `toml:"default"`
		Choices []STTChoice `toml:"choices"`
	} `toml:"stt"`
	VAD struct {
		Model string `toml:"model"`
	} `toml:"vad"`
	WakeWord struct {
		Enable bool   `toml:"enable"`
		Model  string `toml:"model"`
	} `toml:"wake_word"`
	Audio struct {
		Source     string `toml:"source"`
		SilenceMs  int    `toml:"silence_ms"`
		MaxRecordS int    `toml:"max_record_s"`
	} `toml:"audio"`
	History struct {
		Turns        int `toml:"turns"`
		ForgetAfterS int `toml:"forget_after_s"`
	} `toml:"history"`
	// what the llm may use when asked, an empty url turns a tool off
	Tools struct {
		WebSearch    string `toml:"web_search"`
		NotesMCP     string `toml:"notes_mcp"`
		NotesLibrary string `toml:"notes_library"`
	} `toml:"tools"`
	UI struct {
		OpenCmd    []string `toml:"open_cmd"`
		Notify     bool     `toml:"notify"`
		ReadySound string   `toml:"ready_sound"`
	} `toml:"ui"`
}

func Load(path string) (*Config, error) {
	c := &Config{}
	// the same defaults as the module, so a short file works too
	c.LLM.URL = "http://127.0.0.1:11434"
	c.LLM.Model = "jarvis"
	c.LLM.MaxTokens = 200
	c.TTS.URL = c.LLM.URL
	c.Character.Default = "jarvis"
	c.Character.Dir = "/etc/jarvis/characters"
	c.Character.BasePrompt = "/etc/jarvis/prompts/base.md"
	c.STT.Threads = 4
	c.WakeWord.Enable = true
	c.Audio.Source = "default"
	c.Audio.SilenceMs = 1000
	c.Audio.MaxRecordS = 30
	c.History.Turns = 6
	c.History.ForgetAfterS = 600
	c.UI.Notify = true
	c.Tools.NotesLibrary = "notes"
	c.Character.LoreBudget = 2000
	if _, err := toml.DecodeFile(path, c); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if len(c.STT.Choices) == 0 && c.STT.Model != "" {
		c.STT.Choices = []STTChoice{{ID: "whisper", Name: "Whisper small", Kind: "whisper", Dir: c.STT.Model}}
	}
	if !slices.ContainsFunc(c.STT.Choices, func(s STTChoice) bool { return s.ID == c.STT.Default }) && len(c.STT.Choices) > 0 {
		c.STT.Default = c.STT.Choices[0].ID
	}
	if !slices.ContainsFunc(c.LLM.Choices, func(m LLMChoice) bool { return m.Model == c.LLM.Model }) {
		c.LLM.Choices = append([]LLMChoice{{Model: c.LLM.Model, Name: c.LLM.Model}}, c.LLM.Choices...)
	}
	return c, nil
}

// a speech to text model the menu offers: kind is whisper or transducer
// (nemo: parakeet, gigaam), dir its sherpa-onnx folder
type STTChoice struct {
	ID   string `toml:"id" json:"id"`
	Name string `toml:"name" json:"name"`
	Kind string `toml:"kind" json:"-"`
	Dir  string `toml:"dir" json:"-"`
}

// an llm the menu offers: its llama-swap model and what the menu shows
type LLMChoice struct {
	Model string `toml:"model" json:"id"`
	Name  string `toml:"name" json:"name"`
}

// the voice of a character: a custom-voice speaker, or a clone of a clip
type Voice struct {
	Model   string `toml:"model"`
	Speaker string `toml:"speaker"`
	Ref     string `toml:"ref"`
	RefText string `toml:"ref_text"`
}

type Character struct {
	ID    string `toml:"-"`
	Name  string `toml:"name"`
	Wake  string `toml:"wake"`
	Lang  string `toml:"lang"`
	Icon  string `toml:"icon"`
	Voice Voice  `toml:"voice"`
	// persona.md as is
	Persona string `toml:"-"`
	// wake.txt: the wake phrase in the kws model's tokens, the module
	// makes it at build time
	WakeTokens string `toml:"-"`
	// lorebook.json, none without one
	Lore []LoreEntry `toml:"-"`
}

// every folder with a character.toml, sorted by id
func LoadCharacters(dir string) (map[string]*Character, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	chars := map[string]*Character{}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		base := filepath.Join(dir, id)
		ch := &Character{ID: id, Lang: "auto"}
		if _, err := toml.DecodeFile(filepath.Join(base, "character.toml"), ch); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, fmt.Errorf("character %s: %w", id, err)
		}
		persona, err := os.ReadFile(filepath.Join(base, "persona.md"))
		if err != nil {
			return nil, nil, fmt.Errorf("character %s: %w", id, err)
		}
		ch.Persona = string(persona)
		if b, err := os.ReadFile(filepath.Join(base, "wake.txt")); err == nil {
			ch.WakeTokens = strings.TrimSpace(string(b))
		}
		if ch.Lore, err = LoadLorebook(filepath.Join(base, "lorebook.json")); err != nil {
			return nil, nil, fmt.Errorf("character %s: %w", id, err)
		}
		if ch.Name == "" {
			ch.Name = id
		}
		switch ch.Lang {
		case "auto", "en", "ru":
		default:
			return nil, nil, fmt.Errorf("character %s: lang is auto, en or ru, not %q", id, ch.Lang)
		}
		chars[id] = ch
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("no characters in %s", dir)
	}
	return chars, ids, nil
}

// what the user picked last time, over the defaults
type State struct {
	Character string `toml:"character"`
	Source    string `toml:"source"`
	// "small": the cloned voices on the small model
	Clones string `toml:"clones"`
	// the llm's llama-swap model
	LLM string `toml:"llm"`
	// the speech to text model, its id
	STT string `toml:"stt"`
}

func statePath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "jarvis", "state.toml")
}

func LoadState() State {
	var s State
	toml.DecodeFile(statePath(), &s)
	return s
}

func SaveState(s State) error {
	p := statePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p + ".tmp")
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(s); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(p+".tmp", p)
}
