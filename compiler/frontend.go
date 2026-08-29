package compiler

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/alecthomas/participle/v2"
	"github.com/effectus/effectus-go/ast"
)

// parsedSource is the normalized, in-memory result of the compiler front end.
// Callers lower these values but must not mutate them.
type parsedSource struct {
	path string
	file *ast.File
}

// parseSources is the single parser and dialect boundary for legacy and checked
// lowering. It normalizes source identity, predicate text, and flow bindings
// exactly once before either representation is built.
func parseSources(ctx context.Context, sources []Source) ([]parsedSource, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compile front end: context is nil")
	}
	parser, err := participle.Build[ast.File](
		participle.Lexer(ast.Lexer),
		participle.UseLookahead(2),
		participle.Elide("Whitespace", "Comment"),
	)
	if err != nil {
		return nil, fmt.Errorf("compile front end: build parser: %w", err)
	}

	ordered := make([]parsedSource, 0, len(sources))
	seenPaths := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := normalizeSourcePath(source.Path)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenPaths[path]; duplicate {
			return nil, fmt.Errorf("compile front end: duplicate normalized source path %q", path)
		}
		seenPaths[path] = struct{}{}
		extension := filepath.Ext(path)
		if extension != ".eff" && extension != ".effx" {
			return nil, fmt.Errorf("compile front end: source %q must use .eff or .effx", path)
		}
		data, err := checkedSourceBytes(source)
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseBytes(path, data)
		if err != nil {
			return nil, fmt.Errorf("compile front end: parse %s: %w", path, err)
		}
		if err := restoreCheckedPredicateText(file, data); err != nil {
			return nil, fmt.Errorf("compile front end: %s: %w", path, err)
		}
		normalizeCheckedSourceAST(file)
		if err := normalizeFlowBindings(file); err != nil {
			return nil, fmt.Errorf("compile front end: %s: %w", path, err)
		}
		if extension == ".eff" && len(file.Flows) != 0 {
			return nil, fmt.Errorf("compile front end: %s contains flow declarations", path)
		}
		if extension == ".effx" && len(file.Rules) != 0 {
			return nil, fmt.Errorf("compile front end: %s contains list rule declarations", path)
		}
		ordered = append(ordered, parsedSource{path: path, file: file})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].path < ordered[j].path })
	return ordered, nil
}
