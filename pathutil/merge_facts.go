package pathutil

import (
	"fmt"
	"sort"
)

// MergeStrategy controls how multiple fact sources are combined.
type MergeStrategy string

const (
	MergeFirst MergeStrategy = "first" // highest priority source wins
	MergeLast  MergeStrategy = "last"  // lowest priority source wins
	MergeError MergeStrategy = "error" // error on conflicts
)

// SourceProvider couples a fact provider with a name and priority.
type SourceProvider struct {
	Name     string
	Provider FactProvider
	Priority int
}

// MergedFactProvider merges multiple sources for the same namespace.
type MergedFactProvider struct {
	sources  []SourceProvider
	strategy MergeStrategy
}

// NewMergedFactProvider creates a merged provider with the given strategy.
func NewMergedFactProvider(sources []SourceProvider, strategy MergeStrategy) *MergedFactProvider {
	return &MergedFactProvider{
		sources:  normalizeSources(sources),
		strategy: normalizeStrategy(strategy),
	}
}

// Get retrieves a value by applying the merge strategy.
func (m *MergedFactProvider) Get(path string) (interface{}, bool) {
	value, result := m.resolve(path)
	if result == nil || !result.Exists {
		return nil, false
	}
	return value, true
}

// GetWithContext retrieves a value with resolution context.
func (m *MergedFactProvider) GetWithContext(path string) (interface{}, *ResolutionResult) {
	value, result := m.resolve(path)
	if result == nil {
		return nil, &ResolutionResult{Path: path, Exists: false}
	}
	return value, result
}

func (m *MergedFactProvider) resolve(path string) (interface{}, *ResolutionResult) {
	if m == nil || len(m.sources) == 0 {
		return nil, &ResolutionResult{Path: path, Exists: false}
	}

	matches := make([]ResolutionResult, 0)
	for _, source := range m.sources {
		if source.Provider == nil {
			continue
		}
		value, result := source.Provider.GetWithContext(path)
		if result == nil || !result.Exists {
			continue
		}
		result.Path = path
		result.Source = source.Name
		result.Value = value
		matches = append(matches, *result)
	}

	if len(matches) == 0 {
		return nil, &ResolutionResult{Path: path, Exists: false}
	}

	if m.strategy == MergeError && len(matches) > 1 {
		sources := make([]string, 0, len(matches))
		for _, match := range matches {
			sources = append(sources, match.Source)
		}
		return nil, &ResolutionResult{
			Path:    path,
			Exists:  false,
			Sources: sources,
			Error:   fmt.Errorf("conflicting values from sources: %v", sources),
		}
	}

	chosen := matches[0]
	if m.strategy == MergeLast {
		chosen = matches[len(matches)-1]
	}
	chosen.Sources = collectSources(matches)

	return chosen.Value, &chosen
}

func normalizeStrategy(strategy MergeStrategy) MergeStrategy {
	switch strategy {
	case MergeLast, MergeError:
		return strategy
	default:
		return MergeFirst
	}
}

func normalizeSources(sources []SourceProvider) []SourceProvider {
	if len(sources) == 0 {
		return sources
	}

	ordered := make([]SourceProvider, 0, len(sources))
	for _, src := range sources {
		if src.Provider == nil {
			continue
		}
		ordered = append(ordered, src)
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Priority > ordered[j].Priority
	})
	return ordered
}

func collectSources(matches []ResolutionResult) []string {
	seen := make(map[string]struct{})
	for _, match := range matches {
		if match.Source == "" {
			continue
		}
		seen[match.Source] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for source := range seen {
		result = append(result, source)
	}
	sort.Strings(result)
	return result
}
