// lorebook.json: a character's world info, entries that go into the
// prompt only when their keys come up in the conversation. SillyTavern's
// world info and chub's (character book v2) exports both load as is
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type LoreEntry struct {
	Name     string
	Content  string
	Constant bool
	Order    int
	// any of them brings the entry in
	Keys []*regexp.Regexp
	// with Selective set, one of these has to come up too
	Secondary []*regexp.Regexp
}

// Matches: the entry belongs in a prompt about text
func (e LoreEntry) Matches(text string) bool {
	if e.Constant {
		return true
	}
	if !anyMatch(e.Keys, text) {
		return false
	}
	return len(e.Secondary) == 0 || anyMatch(e.Secondary, text)
}

func anyMatch(res []*regexp.Regexp, text string) bool {
	for _, re := range res {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// the fields of both formats, whichever the file has
type rawEntry struct {
	Key           []string `json:"key"`
	Keys          []string `json:"keys"`
	KeySecondary  []string `json:"keysecondary"`
	SecondaryKeys []string `json:"secondary_keys"`
	Content       string   `json:"content"`
	Comment       string   `json:"comment"`
	Name          string   `json:"name"`
	Constant      bool     `json:"constant"`
	Selective     bool     `json:"selective"`
	Disable       bool     `json:"disable"`
	Enabled       *bool    `json:"enabled"`
	Order         *int     `json:"order"`
	InsertOrder   *int     `json:"insertion_order"`
	CaseSensitive *bool    `json:"case_sensitive"`
	CaseSens      *bool    `json:"caseSensitive"`
}

// LoadLorebook: the enabled entries in insertion order, none when the
// file isn't there
func LoadLorebook(path string) ([]LoreEntry, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Entries json.RawMessage `json:"entries"`
		// a whole character card: the book is inside
		Data struct {
			CharacterBook struct {
				Entries json.RawMessage `json:"entries"`
			} `json:"character_book"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	raw := file.Entries
	if len(raw) == 0 {
		raw = file.Data.CharacterBook.Entries
	}
	var list []rawEntry
	// sillytavern keeps them in an object by uid, chub in a list
	var byID map[string]rawEntry
	if err := json.Unmarshal(raw, &byID); err == nil {
		// in uid order, ties in the sort below keep it
		ids := make([]string, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			a, errA := strconv.Atoi(ids[i])
			b, errB := strconv.Atoi(ids[j])
			if errA == nil && errB == nil {
				return a < b
			}
			return ids[i] < ids[j]
		})
		for _, id := range ids {
			list = append(list, byID[id])
		}
	} else if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("%s: entries: %w", path, err)
	}

	var out []LoreEntry
	for _, r := range list {
		if r.Disable || (r.Enabled != nil && !*r.Enabled) || strings.TrimSpace(r.Content) == "" {
			continue
		}
		cs := (r.CaseSensitive != nil && *r.CaseSensitive) || (r.CaseSens != nil && *r.CaseSens)
		e := LoreEntry{
			Name:     firstNonEmpty(r.Name, r.Comment),
			Content:  strings.TrimSpace(r.Content),
			Constant: r.Constant,
			// sillytavern's default
			Order: 100,
		}
		switch {
		case r.InsertOrder != nil:
			e.Order = *r.InsertOrder
		case r.Order != nil:
			e.Order = *r.Order
		}
		if e.Keys, err = compileKeys(append(r.Key, r.Keys...), cs); err != nil {
			return nil, fmt.Errorf("%s: %q: %w", path, e.Name, err)
		}
		if r.Selective {
			if e.Secondary, err = compileKeys(append(r.KeySecondary, r.SecondaryKeys...), cs); err != nil {
				return nil, fmt.Errorf("%s: %q: %w", path, e.Name, err)
			}
		}
		if !e.Constant && len(e.Keys) == 0 {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Constant != out[j].Constant {
			return out[i].Constant
		}
		return out[i].Order < out[j].Order
	})
	return out, nil
}

var regexKey = regexp.MustCompile(`^/(.+)/([a-z]*)$`)

// a key matches at the start of a word, so russian endings still match
// ("машин" finds "машину"). /regex/flags keys are regular expressions
func compileKeys(keys []string, caseSensitive bool) ([]*regexp.Regexp, error) {
	var out []*regexp.Regexp
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		var expr string
		if m := regexKey.FindStringSubmatch(k); m != nil {
			expr = m[1]
			if strings.Contains(m[2], "i") {
				expr = "(?i)" + expr
			}
		} else {
			expr = `(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(k)
			if !caseSensitive {
				expr = "(?i)" + expr
			}
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
