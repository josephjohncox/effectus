package path

import (
	basepath "github.com/effectus/effectus-go/path"
)

// ParsePath parses a string path into a FactPath
func ParsePath(path string) (FactPath, error) {
	basePath, err := basepath.ParseString(path)
	if err != nil {
		return FactPath{}, err
	}

	return FromBasePath(basePath), nil
}

// ValidatePath checks if a path string is valid
func ValidatePath(path string) bool {
	return basepath.ValidatePath(path)
}

// NewSegment creates a simple segment with just a name
func NewSegment(name string) PathSegment {
	return PathSegment{
		Name: name,
	}
}

// NewIndexSegment creates a segment with an array index
func NewIndexSegment(name string, index int) PathSegment {
	return PathSegment{
		Name:  name,
		Index: &index,
	}
}

// NewStringKeySegment creates a segment with a string key
func NewStringKeySegment(name string, key string) PathSegment {
	return PathSegment{
		Name:      name,
		StringKey: &key,
	}
}
