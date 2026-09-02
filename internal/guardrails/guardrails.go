// Package guardrails implements repository surface-area checks.
package guardrails

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const modulePath = "github.com/josephjohncox/effectus"

var (
	recipePattern = regexp.MustCompile(`^([A-Za-z0-9_-]+)(?:\s+[^:]*)?:`)
	expiryPattern = regexp.MustCompile(`Removal deadline: ([0-9]{4}-[0-9]{2}-[0-9]{2})\.?`)
)

// Package describes the fields from go list that the checks use.
type Package struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	Imports    []string
}

// DependencyRule prohibits direct imports from one package pattern to another.
type DependencyRule struct {
	From   string
	To     string
	Reason string
}

// DiscoverModules returns every repository Go module, including untracked modules.
func DiscoverModules(root string) ([]string, error) {
	files, err := includedRepositoryFiles(root, root)
	if err != nil {
		return nil, err
	}
	var modules []string
	for _, path := range files {
		if filepath.Base(path) != "go.mod" {
			continue
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		modules = append(modules, filepath.ToSlash(relative))
	}
	sort.Strings(modules)
	return modules, nil
}

// includedRepositoryFiles returns tracked and untracked files that Git would
// include in the working tree. It intentionally excludes ignored artifacts
// before guardrails inspect their contents.
func includedRepositoryFiles(root, directory string) ([]string, error) {
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		relative = "."
	}
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", filepath.ToSlash(relative))
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list included repository files: %w", err)
	}
	var files []string
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(string(raw)))
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		ignored, err := gitIgnored(root, path)
		if err != nil {
			return nil, err
		}
		if !ignored {
			files = append(files, path)
		}
	}
	return files, nil
}

// gitIgnored applies the repository's complete Git ignore configuration. The
// --no-index flag matters: surface checks must reject ignored generated output
// even when it happens to be present in a developer's index.
func gitIgnored(root, path string) (bool, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	if relative == "." {
		return false, nil
	}
	candidate := filepath.ToSlash(relative)
	command := exec.Command("git", "-C", root, "check-ignore", "--quiet", "--no-index", "--", candidate)
	err = command.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check whether %s is Git-ignored: %w", filepath.ToSlash(relative), err)
}

// ParseRecipes returns the sorted recipe inventory from a Justfile.
func ParseRecipes(data string) []string {
	seen := make(map[string]struct{})
	private := false
	for _, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == "[private]" {
			private = true
			continue
		}
		colon := strings.IndexByte(line, ':')
		if strings.HasPrefix(line, "set ") || colon >= 0 && colon+1 < len(line) && line[colon+1] == '=' {
			continue
		}
		match := recipePattern.FindStringSubmatch(line)
		if len(match) == 2 {
			if !private {
				seen[match[1]] = struct{}{}
			}
			private = false
		}
	}
	return sortedKeys(seen)
}

// MatchPackage reports whether an import path matches an exact or /... pattern.
func MatchPackage(pattern, importPath string) bool {
	if strings.HasSuffix(pattern, "/...") {
		prefix := strings.TrimSuffix(pattern, "/...")
		return importPath == prefix || strings.HasPrefix(importPath, prefix+"/")
	}
	return pattern == importPath
}

// CheckDependencyRules checks direct imports. Transitive dependencies are checked at their own direct edge.
func CheckDependencyRules(packages []Package, rules []DependencyRule) []string {
	var violations []string
	for _, pkg := range packages {
		for _, imported := range pkg.Imports {
			for _, rule := range rules {
				if MatchPackage(rule.From, pkg.ImportPath) && MatchPackage(rule.To, imported) {
					violations = append(violations, fmt.Sprintf("%s imports forbidden %s (%s)", pkg.ImportPath, imported, rule.Reason))
				}
			}
		}
	}
	sort.Strings(violations)
	return violations
}

// CheckForbiddenProductionSymbols rejects removed declarations in non-test Go
// files. Keep this source-level check alongside dependency rules so deleted
// authorities cannot be reintroduced behind an otherwise legal import edge.
func CheckForbiddenProductionSymbols(packages []Package) ([]string, error) {
	forbiddenTypes := map[string]struct{}{
		"ExecutionRuntime": {},
		"CompiledUnit":     {},
		"CompiledSpec":     {},
	}
	var violations []string
	for _, pkg := range packages {
		for _, name := range pkg.GoFiles {
			path := filepath.Join(pkg.Dir, name)
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			for _, declaration := range file.Decls {
				general, ok := declaration.(*ast.GenDecl)
				if !ok || general.Tok != token.TYPE {
					continue
				}
				for _, specification := range general.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if _, forbidden := forbiddenTypes[typeSpec.Name.Name]; forbidden {
						violations = append(violations, fmt.Sprintf("%s declares forbidden type %s", pkg.ImportPath, typeSpec.Name.Name))
					}
					if typeSpec.Name.Name != "CompileOptions" {
						continue
					}
					structure, ok := typeSpec.Type.(*ast.StructType)
					if !ok || structure.Fields == nil {
						continue
					}
					for _, field := range structure.Fields.List {
						for _, fieldName := range field.Names {
							if fieldName.Name == "InspectSource" {
								violations = append(violations, fmt.Sprintf("%s declares forbidden CompileOptions.InspectSource", pkg.ImportPath))
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(violations)
	return violations, nil
}

// CheckDeprecations requires a future removal deadline on hand-written Go deprecations.
func CheckDeprecations(root string, now time.Time) ([]string, error) {
	files, err := includedRepositoryFiles(root, root)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, path := range files {
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, ".pb.go") || generatedPath(root, path) {
			continue
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if !strings.Contains(comment.Text, "Deprecated:") {
					continue
				}
				line := fileSet.Position(comment.Slash).Line
				match := expiryPattern.FindStringSubmatch(comment.Text)
				if len(match) != 2 {
					violations = append(violations, fmt.Sprintf("%s:%d deprecated API has no Removal deadline: YYYY-MM-DD", filepath.ToSlash(relative), line))
					continue
				}
				deadline, err := time.Parse("2006-01-02", match[1])
				if err != nil {
					violations = append(violations, fmt.Sprintf("%s:%d has invalid removal deadline %q", filepath.ToSlash(relative), line, match[1]))
					continue
				}
				if !deadline.After(dateOnly(now)) {
					violations = append(violations, fmt.Sprintf("%s:%d deprecation expired on %s", filepath.ToSlash(relative), line, match[1]))
				}
			}
		}
	}
	sort.Strings(violations)
	return violations, nil
}

func generatedPath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == "gen" {
			return true
		}
	}
	return false
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// CheckCanonicalExecutorDocs rejects stale claims for unsupported outbound resolvers.
func CheckCanonicalExecutorDocs(data string) []string {
	forbidden := []string{
		"supports checked HTTP, gRPC, stream, Kafka, and OCI-resolved targets",
		"uses declarative HTTP, gRPC, stream, Kafka, or OCI-resolved targets",
		"HTTP/stream/gRPC targets",
		"connects each verb to an HTTP, gRPC, stream, or OCI target",
		"## Outbound gRPC verbs",
	}
	var violations []string
	for _, claim := range forbidden {
		if strings.Contains(data, claim) {
			violations = append(violations, "unsupported executor claim: "+claim)
		}
	}
	sort.Strings(violations)
	return violations
}

// CheckCompatibilityDocs requires the documented boundary for unpublished
// compatibility import paths.
func CheckCompatibilityDocs(data string) []string {
	data = strings.Join(strings.Fields(data), " ")
	var violations []string
	for _, required := range []string{
		"first future root release that contains this branch",
		"Published `v0.3.0` does not contain these paths.",
		"just smoke-compat \"$ROOT_VERSION\"",
	} {
		if !strings.Contains(data, required) {
			violations = append(violations, "missing compatibility release boundary: "+required)
		}
	}
	sort.Strings(violations)
	return violations
}

// CheckCompatibilityReleaseClaims rejects affirmative claims that v0.3.0
// publishes a compat/v03 path. It recognizes both release-first and path-first
// wording so a claim cannot evade the check by changing its word order.
func CheckCompatibilityReleaseClaims(data string) []string {
	var violations []string
	normalized := strings.ReplaceAll(strings.ToLower(data), "\r\n", "\n")
	for _, paragraph := range markdownParagraphs.Split(normalized, -1) {
		if !strings.Contains(paragraph, "v0.3.0") || !strings.Contains(paragraph, "compat/v03") {
			continue
		}
		for _, match := range compatibilityClaimActions.FindAllStringIndex(paragraph, -1) {
			// "Published v0.3.0" identifies the release; it is not itself a
			// claim that the release publishes a compatibility path.
			if strings.HasPrefix(strings.TrimSpace(paragraph[match[1]:]), "v0.3.0") {
				continue
			}
			if compatibilityClaimIsNegated(paragraph, match[0], match[1]) {
				continue
			}
			violations = append(violations, "incorrect published compatibility claim: v0.3.0 affirmatively claims a compat/v03 path")
			break
		}
	}
	sort.Strings(violations)
	return uniqueStrings(violations)
}

var (
	markdownParagraphs = regexp.MustCompile(`\n[ \t]*\n`)
	// The first alternatives describe direct release claims. The final three
	// cover equivalent path-first prose such as "is available", "comes with",
	// and "can import".
	compatibilityClaimActions = regexp.MustCompile(`\b(?:publish(?:es|ed)?|contain(?:s|ed)?|includ(?:e|es|ed|ing)|provid(?:e|es|ed|ing)|ship(?:s|ped|ping)?|expos(?:e|es|ed|ing)|offer(?:s|ed|ing)?|list(?:s|ed|ing)?|available|comes\s+with|can\s+import)\b`)
)

func compatibilityClaimIsNegated(paragraph string, actionStart, actionEnd int) bool {
	// Evaluate words around the claim verb, rather than treating any negation
	// in a long paragraph as applying to it. Eight preceding words cover
	// "not among the paths v0.3.0 provides"; four following words cover
	// "v0.3.0 includes no compat/v03 path".
	wordsBefore := strings.Fields(paragraph[:actionStart])
	wordsAfter := strings.Fields(paragraph[actionEnd:])
	return hasCompatibilityNegation(wordsBefore, 8, true) || hasCompatibilityNegation(wordsAfter, 4, false)
}

func hasCompatibilityNegation(words []string, limit int, fromEnd bool) bool {
	if len(words) > limit {
		if fromEnd {
			words = words[len(words)-limit:]
		} else {
			words = words[:limit]
		}
	}
	for _, word := range words {
		switch strings.Trim(word, "`*_.,:;()[]{}") {
		case "not", "never", "neither", "nor", "without", "no", "false":
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

// LoadDependencyRules parses tab-separated from, to, and reason fields.
func LoadDependencyRules(path string) ([]DependencyRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules []DependencyRule
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		parts := strings.Split(text, "\t")
		if len(parts) != 3 {
			return nil, fmt.Errorf("%s:%d: expected three tab-separated fields", path, line)
		}
		rules = append(rules, DependencyRule{From: parts[0], To: parts[1], Reason: parts[2]})
	}
	return rules, scanner.Err()
}

// ModulePath reads the declared module path from a module directory.
func ModulePath(directory string) (string, error) {
	data, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("%s has no module declaration", filepath.Join(directory, "go.mod"))
}

// GoList returns repository packages from the root module.
func GoList(root string) ([]Package, error) {
	return GoListModule(root)
}

// GoListModule returns packages declared by the module in directory.
func GoListModule(directory string) ([]Package, error) {
	command := exec.Command("go", "list", "-json", "./...")
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("go list: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var packages []Package
	for {
		var pkg Package
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		skipped, err := packageInSkippedDirectory(directory, pkg)
		if err != nil {
			return nil, err
		}
		if skipped {
			continue
		}
		packages = append(packages, pkg)
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })
	return packages, nil
}

func packageInSkippedDirectory(moduleDir string, pkg Package) (bool, error) {
	for current := pkg.Dir; current != moduleDir; current = filepath.Dir(current) {
		if filepath.Base(current) == ".git" || filepath.Base(current) == "vendor" {
			return true, nil
		}
	}
	if len(pkg.GoFiles) == 0 {
		return false, nil
	}
	for _, name := range pkg.GoFiles {
		ignored, err := gitIgnored(moduleDir, filepath.Join(pkg.Dir, name))
		if err != nil {
			return false, err
		}
		if !ignored {
			return false, nil
		}
	}
	return true, nil
}

// PublicPackages returns importable root-module packages outside internal directories.
func PublicPackages(packages []Package) []string {
	return PublicPackagesForModule(packages, modulePath)
}

// PublicPackagesForModule returns importable packages for one declared module.
func PublicPackagesForModule(packages []Package, path string) []string {
	var public []string
	for _, pkg := range packages {
		if pkg.Name == "main" || len(pkg.GoFiles) == 0 || pkg.ImportPath != path && !strings.HasPrefix(pkg.ImportPath, path+"/") {
			continue
		}
		if strings.Contains(pkg.ImportPath, "/internal/") || strings.HasSuffix(pkg.ImportPath, "/internal") {
			continue
		}
		public = append(public, pkg.ImportPath)
	}
	sort.Strings(public)
	return public
}

// PublicAPI returns a deterministic source-level inventory of root exported declarations.
func PublicAPI(packages []Package) ([]string, error) {
	return PublicAPIForModule(packages, modulePath)
}

// PublicAPIForModule returns exported declarations for one declared module.
func PublicAPIForModule(packages []Package, path string) ([]string, error) {
	var result []string
	for _, pkg := range packages {
		if !contains(PublicPackagesForModule([]Package{pkg}, path), pkg.ImportPath) {
			continue
		}
		declarations, err := exportedDeclarations(pkg)
		if err != nil {
			return nil, err
		}
		for _, declaration := range declarations {
			result = append(result, pkg.ImportPath+"\t"+declaration)
		}
	}
	sort.Strings(result)
	return result, nil
}

func exportedDeclarations(pkg Package) ([]string, error) {
	fileSet := token.NewFileSet()
	var declarations []string
	for _, name := range pkg.GoFiles {
		path := filepath.Join(pkg.Dir, name)
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return nil, err
		}
		internalImports := internalImportNames(file)
		for _, declaration := range file.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				if !value.Name.IsExported() {
					continue
				}
				prefix := "func "
				if value.Recv != nil && len(value.Recv.List) > 0 {
					receiver := receiverName(value.Recv.List[0].Type)
					if !ast.IsExported(receiver) {
						continue
					}
					prefix = "method " + receiver + "."
				}
				if leaked := exposedInternalImport(value.Type, internalImports); leaked != "" {
					return nil, fmt.Errorf("public declaration %s exposes internal package %s", value.Name.Name, leaked)
				}
				signature := strings.TrimPrefix(nodeString(fileSet, value.Type), "func")
				declarations = append(declarations, prefix+value.Name.Name+signature)
			case *ast.GenDecl:
				for _, rawSpec := range value.Specs {
					switch spec := rawSpec.(type) {
					case *ast.TypeSpec:
						if spec.Name.IsExported() {
							if leaked := exposedInternalImportInType(spec.Type, internalImports); leaked != "" {
								return nil, fmt.Errorf("public declaration %s exposes internal package %s", spec.Name.Name, leaked)
							}
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if name.IsExported() {
								if leaked := exposedInternalImport(spec.Type, internalImports); leaked != "" {
									return nil, fmt.Errorf("public declaration %s exposes internal package %s", name.Name, leaked)
								}
							}
						}
					}
				}
				declarations = append(declarations, exportedGeneralDeclarations(fileSet, value)...)
			}
		}
	}
	sort.Strings(declarations)
	return declarations, nil
}

func internalImportNames(file *ast.File) map[string]string {
	imports := make(map[string]string)
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || !(strings.Contains(path, "/internal/") || strings.HasSuffix(path, "/internal")) {
			continue
		}
		name := filepath.Base(path)
		if imported.Name != nil {
			name = imported.Name.Name
		}
		imports[name] = path
	}
	return imports
}

func exposedInternalImport(node ast.Node, imports map[string]string) string {
	if node == nil {
		return ""
	}
	var leaked string
	ast.Inspect(node, func(node ast.Node) bool {
		if leaked != "" {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		packageName, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		leaked = imports[packageName.Name]
		return leaked == ""
	})
	return leaked
}

func exposedInternalImportInType(expression ast.Expr, imports map[string]string) string {
	switch value := expression.(type) {
	case *ast.StructType:
		for _, field := range value.Fields.List {
			exported := len(field.Names) == 0
			for _, name := range field.Names {
				exported = exported || name.IsExported()
			}
			if exported {
				if leaked := exposedInternalImport(field.Type, imports); leaked != "" {
					return leaked
				}
			}
		}
		return ""
	case *ast.InterfaceType:
		for _, method := range value.Methods.List {
			if len(method.Names) == 0 || method.Names[0].IsExported() {
				if leaked := exposedInternalImport(method.Type, imports); leaked != "" {
					return leaked
				}
			}
		}
		return ""
	default:
		return exposedInternalImport(expression, imports)
	}
}

func exportedGeneralDeclarations(fileSet *token.FileSet, declaration *ast.GenDecl) []string {
	var result []string
	for _, rawSpec := range declaration.Specs {
		switch spec := rawSpec.(type) {
		case *ast.TypeSpec:
			if !spec.Name.IsExported() {
				continue
			}
			result = append(result, "type "+spec.Name.Name+exportedType(fileSet, spec))
		case *ast.ValueSpec:
			for index, name := range spec.Names {
				if !name.IsExported() {
					continue
				}
				text := declaration.Tok.String() + " " + name.Name
				if spec.Type != nil {
					text += " " + nodeString(fileSet, spec.Type)
				}
				if index < len(spec.Values) {
					text += " = " + nodeString(fileSet, spec.Values[index])
				}
				result = append(result, text)
			}
		}
	}
	return result
}

func exportedType(fileSet *token.FileSet, spec *ast.TypeSpec) string {
	assignment := " "
	if spec.Assign.IsValid() {
		assignment = " = "
	}
	switch value := spec.Type.(type) {
	case *ast.StructType:
		var fields []string
		for _, field := range value.Fields.List {
			if len(field.Names) == 0 {
				name := receiverName(field.Type)
				if ast.IsExported(name) {
					fields = append(fields, name+" "+nodeString(fileSet, field.Type))
				}
				continue
			}
			for _, name := range field.Names {
				if name.IsExported() {
					fields = append(fields, name.Name+" "+nodeString(fileSet, field.Type))
				}
			}
		}
		return assignment + "struct{" + strings.Join(fields, "; ") + "}"
	case *ast.InterfaceType:
		var methods []string
		for _, method := range value.Methods.List {
			if len(method.Names) == 0 {
				methods = append(methods, nodeString(fileSet, method.Type))
				continue
			}
			for _, name := range method.Names {
				if name.IsExported() {
					methods = append(methods, name.Name+nodeString(fileSet, method.Type))
				}
			}
		}
		return assignment + "interface{" + strings.Join(methods, "; ") + "}"
	default:
		return assignment + nodeString(fileSet, spec.Type)
	}
}

func receiverName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return nodeString(token.NewFileSet(), expression)
	}
}

func nodeString(fileSet *token.FileSet, node any) string {
	var output bytes.Buffer
	if err := format.Node(&output, fileSet, node); err != nil {
		return "<invalid>"
	}
	return strings.Join(strings.Fields(output.String()), " ")
}

// CheckBudgetCounts verifies that the Current column in BUDGETS.md agrees
// with the counts derived from the reviewed inventories.
func CheckBudgetCounts(data string, inventoryCounts map[string]int) []string {
	current := make(map[string]int, len(inventoryCounts))
	var violations []string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			continue
		}
		columns := strings.Split(strings.Trim(line, "|"), "|")
		if len(columns) != 4 {
			continue
		}
		for index := range columns {
			columns[index] = strings.TrimSpace(columns[index])
		}
		surface := columns[0]
		inventoryCount, tracked := inventoryCounts[surface]
		if !tracked {
			continue
		}
		if _, duplicate := current[surface]; duplicate {
			violations = append(violations, fmt.Sprintf("budget %q has multiple Current values", surface))
			continue
		}
		budgetCount, err := strconv.Atoi(columns[2])
		if err != nil {
			violations = append(violations, fmt.Sprintf("budget %q has invalid Current value %q", surface, columns[2]))
			continue
		}
		current[surface] = budgetCount
		if budgetCount != inventoryCount {
			violations = append(violations, fmt.Sprintf("budget %q Current=%d; inventory=%d", surface, budgetCount, inventoryCount))
		}
	}
	surfaces := make([]string, 0, len(inventoryCounts))
	for surface := range inventoryCounts {
		surfaces = append(surfaces, surface)
	}
	sort.Strings(surfaces)
	for _, surface := range surfaces {
		if _, found := current[surface]; !found {
			violations = append(violations, fmt.Sprintf("budget %q has no Current value", surface))
		}
	}
	sort.Strings(violations)
	return violations
}

// CompareInventory returns a useful error when expected and actual lines differ.
func CompareInventory(name string, expected, actual []string) error {
	if strings.Join(expected, "\n") == strings.Join(actual, "\n") {
		return nil
	}
	missing, added := difference(expected, actual), difference(actual, expected)
	return fmt.Errorf("%s changed; removed=%v added=%v (update only with an intentional surface review)", name, missing, added)
}

func difference(left, right []string) []string {
	set := make(map[string]struct{}, len(right))
	for _, item := range right {
		set[item] = struct{}{}
	}
	var result []string
	for _, item := range left {
		if _, ok := set[item]; !ok {
			result = append(result, item)
		}
	}
	return result
}

// ReadInventory reads nonempty, non-comment lines.
func ReadInventory(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// WriteInventory writes a generated inventory with its review warning.
func WriteInventory(path, description string, values []string) error {
	data := "# " + description + "\n# Do not update this file without an intentional surface review.\n" + strings.Join(values, "\n") + "\n"
	return os.WriteFile(path, []byte(data), 0o644)
}

// DiscoverExamples returns immediate runnable example directories. Shared assets
// without Go source remain under examples but are not product examples.
func DiscoverExamples(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "examples"))
	if err != nil {
		return nil, err
	}
	var examples []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		contents, readErr := os.ReadDir(filepath.Join(root, "examples", entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		runnable := false
		for _, child := range contents {
			if !child.IsDir() && strings.HasSuffix(child.Name(), ".go") {
				runnable = true
				break
			}
		}
		if !runnable {
			if _, scriptErr := os.Stat(filepath.Join(root, "examples", entry.Name(), "scripts", "run.sh")); scriptErr == nil {
				runnable = true
			} else if !os.IsNotExist(scriptErr) {
				return nil, scriptErr
			}
		}
		if runnable {
			examples = append(examples, entry.Name())
		}
	}
	sort.Strings(examples)
	return examples, nil
}

// TopLevelDirectories returns reviewed product directories that contain at
// least one included source or configuration asset. Untracked assets are
// included deliberately; generated output is excluded by Git ignore rules.
func TopLevelDirectories(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var directories []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".git" || entry.Name() == "vendor" {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		hasAsset, err := directoryHasIncludedAsset(root, directory)
		if err != nil {
			return nil, err
		}
		if hasAsset {
			directories = append(directories, entry.Name())
		}
	}
	sort.Strings(directories)
	return directories, nil
}

func directoryHasIncludedAsset(root, directory string) (bool, error) {
	files, err := includedRepositoryFiles(root, directory)
	if err != nil {
		return false, err
	}
	for _, path := range files {
		if sourceOrConfigAsset(filepath.Base(path)) {
			return true, nil
		}
	}
	return false, nil
}

func sourceOrConfigAsset(name string) bool {
	switch name {
	case "Dockerfile", "Makefile", "Justfile", "justfile", "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "Cargo.toml", "Cargo.lock", "Gemfile", "Rakefile":
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".c", ".cc", ".cfg", ".css", ".cjs", ".cpp", ".cs", ".eff", ".effx", ".ex", ".exs", ".go", ".gql", ".graphql", ".h", ".hpp", ".html", ".ini", ".java", ".js", ".json", ".jsx", ".kt", ".kts", ".lua", ".md", ".mdc", ".mjs", ".php", ".proto", ".py", ".rb", ".rs", ".sh", ".sql", ".swift", ".tf", ".tfvars", ".tla", ".toml", ".ts", ".tsx", ".txt", ".xml", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
