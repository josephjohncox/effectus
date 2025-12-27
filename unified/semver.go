package unified

import (
	"fmt"
	"strconv"
	"strings"
)

type semVersion struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

type semComparator struct {
	op      string
	version semVersion
}

func parseSemVersion(raw string) (semVersion, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return semVersion{}, fmt.Errorf("empty version")
	}
	trimmed = strings.TrimPrefix(trimmed, "v")
	trimmed = strings.TrimPrefix(trimmed, "V")

	pre := ""
	if idx := strings.IndexAny(trimmed, "-+"); idx != -1 {
		pre = trimmed[idx+1:]
		trimmed = trimmed[:idx]
	}

	parts := strings.Split(trimmed, ".")
	if len(parts) > 3 {
		parts = parts[:3]
	}

	vals := []int{0, 0, 0}
	for i := 0; i < len(parts); i++ {
		if parts[i] == "" {
			return semVersion{}, fmt.Errorf("invalid version segment")
		}
		val, err := strconv.Atoi(parts[i])
		if err != nil {
			return semVersion{}, fmt.Errorf("invalid version segment %q", parts[i])
		}
		vals[i] = val
	}

	return semVersion{Major: vals[0], Minor: vals[1], Patch: vals[2], Pre: pre}, nil
}

func compareSemVersion(a, b semVersion) int {
	if a.Major != b.Major {
		return compareInt(a.Major, b.Major)
	}
	if a.Minor != b.Minor {
		return compareInt(a.Minor, b.Minor)
	}
	if a.Patch != b.Patch {
		return compareInt(a.Patch, b.Patch)
	}
	if a.Pre == "" && b.Pre != "" {
		return 1
	}
	if a.Pre != "" && b.Pre == "" {
		return -1
	}
	if a.Pre == b.Pre {
		return 0
	}
	if a.Pre < b.Pre {
		return -1
	}
	return 1
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func satisfiesConstraint(versionRaw, constraintRaw string) (bool, error) {
	constraint := strings.TrimSpace(constraintRaw)
	if constraint == "" || constraint == "*" {
		return true, nil
	}

	version, err := parseSemVersion(versionRaw)
	if err != nil {
		return false, err
	}

	groups := strings.Split(constraint, "||")
	for _, group := range groups {
		if groupSatisfied(version, group) {
			return true, nil
		}
	}

	return false, nil
}

func groupSatisfied(version semVersion, group string) bool {
	tokens := splitConstraintTokens(group)
	if len(tokens) == 0 {
		return true
	}

	comparators := make([]semComparator, 0)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		parsed, ok := expandConstraintToken(token)
		if !ok {
			return false
		}
		comparators = append(comparators, parsed...)
	}

	for _, comp := range comparators {
		if !compareConstraint(version, comp) {
			return false
		}
	}
	return true
}

func splitConstraintTokens(group string) []string {
	group = strings.ReplaceAll(group, ",", " ")
	return strings.Fields(group)
}

func expandConstraintToken(token string) ([]semComparator, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, true
	}

	if strings.HasPrefix(token, "^") {
		base, err := parseSemVersion(token[1:])
		if err != nil {
			return nil, false
		}
		upper := caretUpperBound(base)
		return []semComparator{
			{op: ">=", version: base},
			{op: "<", version: upper},
		}, true
	}

	if strings.HasPrefix(token, "~") {
		base, err := parseSemVersion(token[1:])
		if err != nil {
			return nil, false
		}
		upper := tildeUpperBound(base, token[1:])
		return []semComparator{
			{op: ">=", version: base},
			{op: "<", version: upper},
		}, true
	}

	if strings.Contains(token, "x") || strings.Contains(token, "X") || strings.Contains(token, "*") {
		lower, upper, ok := wildcardRange(token)
		if !ok {
			return nil, false
		}
		if upper == nil {
			return nil, true
		}
		return []semComparator{
			{op: ">=", version: *lower},
			{op: "<", version: *upper},
		}, true
	}

	op := "="
	versionRaw := token
	for _, prefix := range []string{">=", "<=", "!=", ">", "<", "="} {
		if strings.HasPrefix(token, prefix) {
			op = prefix
			versionRaw = strings.TrimSpace(strings.TrimPrefix(token, prefix))
			break
		}
	}

	version, err := parseSemVersion(versionRaw)
	if err != nil {
		return nil, false
	}

	return []semComparator{{op: op, version: version}}, true
}

func caretUpperBound(base semVersion) semVersion {
	if base.Major > 0 {
		return semVersion{Major: base.Major + 1}
	}
	if base.Minor > 0 {
		return semVersion{Major: 0, Minor: base.Minor + 1}
	}
	return semVersion{Major: 0, Minor: 0, Patch: base.Patch + 1}
}

func tildeUpperBound(base semVersion, raw string) semVersion {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(raw), "v"), ".")
	if len(parts) <= 1 {
		return semVersion{Major: base.Major + 1}
	}
	return semVersion{Major: base.Major, Minor: base.Minor + 1}
}

func wildcardRange(token string) (*semVersion, *semVersion, bool) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "*" || trimmed == "x" || trimmed == "X" {
		return nil, nil, true
	}

	trimmed = strings.TrimPrefix(trimmed, "v")
	trimmed = strings.TrimPrefix(trimmed, "V")
	parts := strings.Split(trimmed, ".")
	for len(parts) < 3 {
		parts = append(parts, "x")
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, nil, false
	}

	if isWildcard(parts[1]) {
		lower := semVersion{Major: major}
		upper := semVersion{Major: major + 1}
		return &lower, &upper, true
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, nil, false
	}

	if isWildcard(parts[2]) {
		lower := semVersion{Major: major, Minor: minor}
		upper := semVersion{Major: major, Minor: minor + 1}
		return &lower, &upper, true
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, nil, false
	}
	lower := semVersion{Major: major, Minor: minor, Patch: patch}
	upper := semVersion{Major: major, Minor: minor, Patch: patch + 1}
	return &lower, &upper, true
}

func isWildcard(part string) bool {
	switch strings.ToLower(strings.TrimSpace(part)) {
	case "x", "*":
		return true
	default:
		return false
	}
}

func compareConstraint(version semVersion, comp semComparator) bool {
	cmp := compareSemVersion(version, comp.version)
	switch comp.op {
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case "!=":
		return cmp != 0
	case "=":
		return cmp == 0
	default:
		return false
	}
}

func selectHighestVersion(versions []string, constraint string) (string, error) {
	var best string
	var bestParsed semVersion
	bestSet := false

	for _, versionRaw := range versions {
		parsed, err := parseSemVersion(versionRaw)
		if err != nil {
			continue
		}
		ok, err := satisfiesConstraint(versionRaw, constraint)
		if err != nil || !ok {
			continue
		}
		if !bestSet || compareSemVersion(parsed, bestParsed) > 0 {
			best = versionRaw
			bestParsed = parsed
			bestSet = true
		}
	}

	if !bestSet {
		return "", fmt.Errorf("no version satisfies constraint %q", constraint)
	}
	return best, nil
}
