// Package uilocale selects the language of the host application.
package uilocale

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const Default = "zh-CN"
const System = "system"
const PreferenceFile = "interface-language.json"

type Preference struct {
	Language string `json:"language"`
}

func Load(profile string) Preference {
	var pref Preference
	if data, err := os.ReadFile(filepath.Join(profile, PreferenceFile)); err == nil &&
		json.Unmarshal(data, &pref) == nil && Valid(pref.Language) {
		return pref
	}
	return Preference{Language: System}
}

func (p Preference) Active(system string) string {
	if p.Language == System {
		return system
	}
	return p.Language
}

//go:embed languages.json
var registry []byte

var supported = func() map[string]string {
	var languages []struct{ ID string }
	if err := json.Unmarshal(registry, &languages); err != nil {
		panic(err)
	}
	index := make(map[string]string, len(languages))
	for _, language := range languages {
		index[strings.ToLower(language.ID)] = language.ID
	}
	return index
}()

func Valid(id string) bool {
	return id == System || id != "" && supported[strings.ToLower(id)] == id
}

// English is the interface for a host that names only other languages; a host
// that names none keeps the Chinese source text.
const fallback = "en"

func Match(preferred []string) string {
	named := false
	for _, tag := range preferred {
		tag, _, _ = strings.Cut(tag, ".")
		tag, _, _ = strings.Cut(tag, "@")
		tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
		primary, _, _ := strings.Cut(tag, "-")
		if primary == "" || primary == "c" || primary == "posix" {
			continue
		}
		named = true
		if primary == "zh" {
			return Default
		}
		if id, ok := supported[primary]; ok {
			return id
		}
	}
	if named {
		return fallback
	}
	return Default
}

func Host() string { return Match(hostLanguages()) }
