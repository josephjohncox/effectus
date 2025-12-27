package types

import "strings"

// RegisterNamespaceAlias registers an alias for a canonical prefix.
func (ts *TypeSystem) RegisterNamespaceAlias(alias, target string) {
	alias = strings.TrimSpace(alias)
	target = strings.TrimSpace(target)
	if alias == "" || target == "" {
		return
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.aliases == nil {
		ts.aliases = make(map[string]string)
	}
	ts.aliases[alias] = target
}

// ResolveAlias resolves a path using registered aliases, if any.
func (ts *TypeSystem) ResolveAlias(path string) string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return resolveAliasPath(path, ts.aliases)
}

func resolveAliasPath(path string, aliases map[string]string) string {
	if len(aliases) == 0 || path == "" {
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
