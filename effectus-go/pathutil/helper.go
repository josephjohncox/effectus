package pathutil

import (
	"fmt"
)

// ToPath converts different types to a Path object
// This is a helper for the transition from string paths to Path objects
func ToPath(path interface{}) (Path, error) {
	switch p := path.(type) {
	case string:
		if p == "" {
			return Path{}, fmt.Errorf("empty path")
		}
		return ParseString(p)
	case Path:
		return p, nil
	default:
		return Path{}, fmt.Errorf("unsupported path type: %T", path)
	}
}

// ToString safely converts a Path to a string
func ToString(path Path) string {
	return path.String()
}
