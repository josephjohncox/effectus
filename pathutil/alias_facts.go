package pathutil

import (
	"strings"

	"github.com/effectus/effectus-go"
)

// AliasedFacts resolves namespace aliases before delegating to the base facts.
type AliasedFacts struct {
	Base    effectus.Facts
	Aliases map[string]string
}

// Get returns the value at the resolved path.
func (a *AliasedFacts) Get(path string) (interface{}, bool) {
	if a == nil || a.Base == nil {
		return nil, false
	}
	resolved := resolveAlias(path, a.Aliases)
	return a.Base.Get(resolved)
}

// Schema returns the underlying schema info.
func (a *AliasedFacts) Schema() effectus.SchemaInfo {
	if a == nil || a.Base == nil {
		return nil
	}
	return a.Base.Schema()
}

// NewAliasedFacts wraps facts with namespace aliases.
func NewAliasedFacts(base effectus.Facts, aliases map[string]string) *AliasedFacts {
	return &AliasedFacts{Base: base, Aliases: aliases}
}

func resolveAlias(path string, aliases map[string]string) string {
	if path == "" || len(aliases) == 0 {
		return path
	}
	for alias, target := range aliases {
		if path == alias {
			return target
		}
		prefix := alias + "."
		if strings.HasPrefix(path, prefix) {
			return target + strings.TrimPrefix(path, alias)
		}
	}
	return path
}
