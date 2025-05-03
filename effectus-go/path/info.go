package path

// PathInfo represents a path to a fact or other value in the system
type PathInfo interface {
	// Namespace returns the namespace of this path
	Namespace() string

	// Elements returns the path elements (excluding namespace)
	Elements() []PathElement

	// String returns the full path as a dot-separated string
	String() string
}

// SimplePath is a simple implementation of PathInfo
type SimplePath struct {
	namespace string
	elements  []PathElement
}

// Namespace returns the namespace of this path
func (p SimplePath) Namespace() string {
	return p.namespace
}

// Elements returns the path elements (excluding namespace)
func (p SimplePath) Elements() []PathElement {
	return p.elements
}

// String returns the full path as a dot-separated string
func (p SimplePath) String() string {
	return RenderPath(p.namespace, p.elements)
}

// NewSimplePath creates a new SimplePath with given namespace and elements
func NewSimplePath(namespace string, elements []PathElement) SimplePath {
	return SimplePath{
		namespace: namespace,
		elements:  elements,
	}
}

// SimplePathFromString parses a string path into a SimplePath
func SimplePathFromString(pathStr string) (SimplePath, error) {
	basePath, err := ParseString(pathStr)
	if err != nil {
		return SimplePath{}, err
	}

	return SimplePath{
		namespace: basePath.Namespace,
		elements:  basePath.Elements,
	}, nil
}

// PathToString converts a PathInfo to its string representation
func PathToString(p PathInfo) string {
	if p == nil {
		return ""
	}
	return p.String()
}

// StringToElements converts a string path to elements
func StringToElements(pathStr string) (string, []PathElement, error) {
	return ParsePath(pathStr)
}
