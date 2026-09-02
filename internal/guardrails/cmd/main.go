package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/josephjohncox/effectus/internal/guardrails"
)

func main() {
	root, err := repositoryRoot()
	if err != nil {
		fatal(err)
	}
	command := "check"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "check":
		err = check(root)
	case "modules":
		err = printModules(root)
	case "snapshot":
		err = snapshot(root)
	default:
		err = fmt.Errorf("unknown command %q (use check, modules, or snapshot)", command)
	}
	if err != nil {
		fatal(err)
	}
}

func repositoryRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not find repository go.mod")
		}
		current = parent
	}
}

func check(root string) error {
	modules, err := guardrails.DiscoverModules(root)
	if err != nil {
		return err
	}
	packages, err := allPackages(root, modules)
	if err != nil {
		return err
	}
	checks := []struct {
		name   string
		path   string
		actual func() ([]string, error)
	}{
		{
			name:   "module inventory",
			path:   filepath.Join(root, "guardrails", "modules.txt"),
			actual: func() ([]string, error) { return modules, nil },
		},
		{
			name:   "public package inventory",
			path:   filepath.Join(root, "guardrails", "public-packages.txt"),
			actual: func() ([]string, error) { return publicPackages(root, modules, packages) },
		},
		{
			name:   "public API inventory",
			path:   filepath.Join(root, "guardrails", "public-api.txt"),
			actual: func() ([]string, error) { return publicAPI(root, modules, packages) },
		},
		{
			name:   "example inventory",
			path:   filepath.Join(root, "guardrails", "examples.txt"),
			actual: func() ([]string, error) { return guardrails.DiscoverExamples(root) },
		},
		{
			name:   "top-level directory inventory",
			path:   filepath.Join(root, "guardrails", "top-level.txt"),
			actual: func() ([]string, error) { return guardrails.TopLevelDirectories(root) },
		},
		{
			name: "Just recipe inventory",
			path: filepath.Join(root, "guardrails", "just-recipes.txt"),
			actual: func() ([]string, error) {
				data, err := os.ReadFile(filepath.Join(root, "justfile"))
				if err != nil {
					return nil, err
				}
				return guardrails.ParseRecipes(string(data)), nil
			},
		},
	}
	var failures []string
	for _, item := range checks {
		expected, readErr := guardrails.ReadInventory(item.path)
		if readErr != nil {
			failures = append(failures, readErr.Error())
			continue
		}
		actual, actualErr := item.actual()
		if actualErr != nil {
			failures = append(failures, actualErr.Error())
			continue
		}
		if compareErr := guardrails.CompareInventory(item.name, expected, actual); compareErr != nil {
			failures = append(failures, compareErr.Error())
		}
	}
	budgetCounts, budgetErr := budgetInventoryCounts(root)
	if budgetErr != nil {
		failures = append(failures, budgetErr.Error())
	} else if data, readErr := os.ReadFile(filepath.Join(root, "guardrails", "BUDGETS.md")); readErr != nil {
		failures = append(failures, readErr.Error())
	} else {
		for _, violation := range guardrails.CheckBudgetCounts(string(data), budgetCounts) {
			failures = append(failures, "guardrails/BUDGETS.md: "+violation)
		}
	}

	rules, err := guardrails.LoadDependencyRules(filepath.Join(root, "guardrails", "forbidden-dependencies.tsv"))
	if err != nil {
		failures = append(failures, err.Error())
	} else {
		failures = append(failures, guardrails.CheckDependencyRules(packages, rules)...)
	}
	forbiddenSymbols, err := guardrails.CheckForbiddenProductionSymbols(packages)
	if err != nil {
		failures = append(failures, err.Error())
	} else {
		failures = append(failures, forbiddenSymbols...)
	}
	deprecations, err := guardrails.CheckDeprecations(root, time.Now().UTC())
	if err != nil {
		failures = append(failures, err.Error())
	} else {
		failures = append(failures, deprecations...)
	}
	docClaims, err := documentationClaims(root)
	if err != nil {
		failures = append(failures, err.Error())
	} else {
		failures = append(failures, docClaims...)
	}
	if err := documentedCLITestContract(root); err != nil {
		failures = append(failures, err.Error())
	}
	catalog, err := exampleCatalog(guardrails.DiscoverExamples(root))
	if err != nil {
		failures = append(failures, err.Error())
	} else if data, readErr := os.ReadFile(filepath.Join(root, "examples", "README.md")); readErr != nil {
		failures = append(failures, readErr.Error())
	} else if string(data) != catalog {
		failures = append(failures, "example catalog is stale; run go run ./internal/guardrails/cmd snapshot")
	}
	if len(failures) > 0 {
		return fmt.Errorf("repository guardrails failed:\n- %s", strings.Join(failures, "\n- "))
	}
	fmt.Println("repository guardrails passed")
	return nil
}

// documentedCLITestContract prevents a future surface reduction from silently
// deleting the executable documentation contracts that CI runs through guardrails.
func documentedCLITestContract(root string) error {
	for _, path := range []string{
		filepath.Join(root, "cmd", "effectusc", "docs_contract_test.go"),
		filepath.Join(root, "cmd", "effectusd", "docs_contract_test.go"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("required documented CLI contract test is absent: %s", path)
		}
		if !strings.Contains(string(data), "func TestDocumentedCLIAndFlags") {
			return fmt.Errorf("documented CLI contract test matches nothing: %s", path)
		}
	}
	return nil
}

func printModules(root string) error {
	modules, err := guardrails.DiscoverModules(root)
	if err != nil {
		return err
	}
	for _, module := range modules {
		fmt.Println(module)
	}
	return nil
}

func snapshot(root string) error {
	modules, err := guardrails.DiscoverModules(root)
	if err != nil {
		return err
	}
	packages, err := allPackages(root, modules)
	if err != nil {
		return err
	}
	api, err := publicAPI(root, modules, packages)
	if err != nil {
		return err
	}
	examples, err := guardrails.DiscoverExamples(root)
	if err != nil {
		return err
	}
	topLevel, err := guardrails.TopLevelDirectories(root)
	if err != nil {
		return err
	}
	justfile, err := os.ReadFile(filepath.Join(root, "justfile"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "guardrails"), 0o755); err != nil {
		return err
	}
	public, err := publicPackages(root, modules, packages)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		name        string
		description string
		values      []string
	}{
		{"modules.txt", "Approved Go modules.", modules},
		{"public-packages.txt", "Approved importable packages.", public},
		{"public-api.txt", "Approved exported Go declarations.", api},
		{"examples.txt", "Approved immediate example directories.", examples},
		{"top-level.txt", "Approved top-level product directories.", topLevel},
		{"just-recipes.txt", "Approved visible Just recipe names.", guardrails.ParseRecipes(string(justfile))},
	} {
		if err := guardrails.WriteInventory(filepath.Join(root, "guardrails", item.name), item.description, item.values); err != nil {
			return err
		}
	}
	catalog, err := exampleCatalog(examples, nil)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "examples", "README.md"), []byte(catalog), 0o644); err != nil {
		return err
	}
	fmt.Println("guardrail inventories updated")
	return nil
}

func exampleCatalog(examples []string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	descriptions := map[string]string{
		"embedded_orders":     "Embedded implementation of the order-review scenario.",
		"standalone_executor": "Durable implementation of the same scenario with a separate HTTP executor.",
		"grpc_execution":      "Inbound generated gRPC client reference.",
	}
	var body strings.Builder
	body.WriteString("# Effectus Example Catalog\n\n<!-- Generated from guardrails/examples.txt; do not edit manually. -->\n\n| Directory | Role |\n| --- | --- |\n")
	for _, example := range examples {
		description, ok := descriptions[example]
		if !ok {
			return "", fmt.Errorf("example %q has no catalog description", example)
		}
		fmt.Fprintf(&body, "| `%s` | %s |\n", example, description)
	}
	body.WriteString("\nGetting Started is the executable authority for commands and prerequisites.\n")
	return body.String(), nil
}

func budgetInventoryCounts(root string) (map[string]int, error) {
	inventories := map[string]string{
		"Immediate examples":      "examples.txt",
		"Visible Just recipes":    "just-recipes.txt",
		"Go modules":              "modules.txt",
		"Product package domains": "public-packages.txt",
	}
	counts := make(map[string]int, len(inventories))
	for surface, name := range inventories {
		inventory, err := guardrails.ReadInventory(filepath.Join(root, "guardrails", name))
		if err != nil {
			return nil, err
		}
		if surface == "Product package domains" {
			module, err := guardrails.ModulePath(root)
			if err != nil {
				return nil, err
			}
			counts[surface] = productPackageDomainCount(inventory, module)
			continue
		}
		counts[surface] = len(inventory)
	}
	return counts, nil
}

// productPackageDomainCount includes root import paths and explicit v0.3
// compatibility packages, but not generated or implementation subpackages.
func productPackageDomainCount(packages []string, module string) int {
	domains := make(map[string]struct{})
	for _, path := range packages {
		if path == module {
			domains[path] = struct{}{}
			continue
		}
		relative := strings.TrimPrefix(path, module+"/")
		if relative == path {
			continue
		}
		parts := strings.Split(relative, "/")
		if len(parts) == 1 || len(parts) == 3 && parts[0] == "compat" && parts[1] == "v03" {
			domains[path] = struct{}{}
		}
	}
	return len(domains)
}

func documentationClaims(root string) ([]string, error) {
	var violations []string
	compatibilityPath := filepath.Join(root, "docs", "COMPATIBILITY.md")
	compatibility, err := os.ReadFile(compatibilityPath)
	if err != nil {
		return nil, err
	}
	for _, claim := range guardrails.CheckCompatibilityDocs(string(compatibility)) {
		violations = append(violations, "docs/COMPATIBILITY.md: "+claim)
	}
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && skippedDocumentationDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, claim := range guardrails.CheckCanonicalExecutorDocs(string(data)) {
			violations = append(violations, filepath.ToSlash(relative)+": "+claim)
		}
		for _, claim := range guardrails.CheckCompatibilityReleaseClaims(string(data)) {
			violations = append(violations, filepath.ToSlash(relative)+": "+claim)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(violations)
	return violations, nil
}

func skippedDocumentationDirectory(name string) bool {
	switch name {
	case ".git", ".pi", ".codebase-index", ".tools", "node_modules", "vendor", "bin", "out", "site":
		return true
	default:
		return false
	}
}

func allPackages(root string, modules []string) ([]guardrails.Package, error) {
	var packages []guardrails.Package
	for _, module := range modules {
		directory := filepath.Join(root, module)
		listed, err := guardrails.GoListModule(directory)
		if err != nil {
			return nil, fmt.Errorf("list module %s: %w", module, err)
		}
		packages = append(packages, listed...)
	}
	return packages, nil
}

func publicPackages(root string, modules []string, packages []guardrails.Package) ([]string, error) {
	var public []string
	for _, module := range modules {
		path, err := guardrails.ModulePath(filepath.Join(root, module))
		if err != nil {
			return nil, err
		}
		public = append(public, guardrails.PublicPackagesForModule(packages, path)...)
	}
	return uniqueSorted(public), nil
}

func publicAPI(root string, modules []string, packages []guardrails.Package) ([]string, error) {
	var api []string
	for _, module := range modules {
		path, err := guardrails.ModulePath(filepath.Join(root, module))
		if err != nil {
			return nil, err
		}
		declared, err := guardrails.PublicAPIForModule(packages, path)
		if err != nil {
			return nil, err
		}
		api = append(api, declared...)
	}
	return uniqueSorted(api), nil
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	os.Exit(1)
}
