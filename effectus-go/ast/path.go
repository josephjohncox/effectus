package ast

import (
	"github.com/effectus/effectus-go/unified/pathutil"
)

// ResolvePathExpressions resolves all path expressions in a parsed file
func ResolvePathExpressions(file *File) error {
	// Process rules
	for _, rule := range file.Rules {
		if rule.When != nil {
			for _, pred := range rule.When.Predicates {
				if err := resolvePathExpression(pred.PathExpr); err != nil {
					return err
				}
			}
		}
	}

	// Process flows
	for _, flow := range file.Flows {
		if flow.When != nil {
			for _, pred := range flow.When.Predicates {
				if err := resolvePathExpression(pred.PathExpr); err != nil {
					return err
				}
			}
		}

		if flow.Steps != nil {
			for _, step := range flow.Steps.Steps {
				for _, arg := range step.Args {
					if arg.Value != nil && arg.Value.PathExpr != nil {
						if err := resolvePathExpression(arg.Value.PathExpr); err != nil {
							return err
						}
					}
				}
			}
		}
	}

	return nil
}

// resolvePathExpression resolves a single path expression
func resolvePathExpression(pathExpr *PathExpression) error {
	if pathExpr == nil || pathExpr.Raw == "" {
		return nil
	}

	// Use the shared path parser to parse the path
	namespace, elements, err := pathutil.ParsePath(pathExpr.Raw)
	if err != nil {
		return err
	}

	// Set the namespace and segments
	pathExpr.Namespace = namespace

	// Initialize segments slice
	pathExpr.Segments = make([]string, len(elements))
	for i, elem := range elements {
		pathExpr.Segments[i] = elem.String()
	}

	// Initialize indexed segments
	pathExpr.IndexedSegments = make([]PathSegmentInfo, len(elements))
	for i, elem := range elements {
		var index *int
		if elem.HasIndex() {
			val := elem.Index()
			index = &val
		}

		pathExpr.IndexedSegments[i] = PathSegmentInfo{
			Name:  elem.Name(),
			Index: index,
		}
	}

	return nil
}

// GetFullPath returns the full path string, reconstructed from namespace and segments
func (p *PathExpression) GetFullPath() string {
	if p == nil {
		return ""
	}

	// If we already have the raw path, return it
	if p.Raw != "" {
		return p.Raw
	}

	// Otherwise, construct it from namespace and segments
	elements := make([]pathutil.SimplePathElement, len(p.IndexedSegments))
	for i, seg := range p.IndexedSegments {
		elements[i] = pathutil.NewPathElement(seg.Name, seg.Index)
	}

	return pathutil.RenderPath(p.Namespace, elements)
}
