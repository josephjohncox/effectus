package compiler

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/alecthomas/participle/v2"
	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/internal/language/ast"
)

// parsedSource is the normalized, in-memory result of the compiler front end.
// Callers lower these values but must not mutate them.
type parsedSource struct {
	path string
	file *ast.File
}

func normalizeFlowBindings(file *ast.File) error {
	if file == nil {
		return nil
	}
	for _, flow := range file.Flows {
		if flow == nil || flow.Steps == nil {
			continue
		}
		for _, step := range flow.Steps.Steps {
			if step == nil || step.Arrow == "" {
				continue
			}
			if step.BindName != "" {
				return fmt.Errorf("%d:%d: step cannot use both prefix and arrow bindings", step.Pos.Line, step.Pos.Column)
			}
			step.BindName, step.Arrow = step.Arrow, ""
		}
	}
	return nil
}

// parseSources is the checked compiler's parser and dialect boundary. It
// normalizes source identity, predicate text, and flow bindings before IR is
// lowered. SourceBundle has already validated source paths and UTF-8 content.
func parseSources(ctx context.Context, sources []bundle.Source) ([]parsedSource, error) {
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
		data := []byte(source.Content)
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
