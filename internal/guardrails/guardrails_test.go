package guardrails

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDiscoverModulesIncludesNewModulesAndSkipsToolState(t *testing.T) {
	root := newGitRepository(t, ".pi/\nvendor/\n")
	for _, path := range []string{"go.mod", "examples/go.mod", "tools/new/go.mod", ".pi/worktree/go.mod", "vendor/dependency/go.mod"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("module test\n"), 0o600))
	}
	modules, err := DiscoverModules(root)
	require.NoError(t, err)
	require.Equal(t, []string{".", "examples", "tools/new"}, modules)
}

func TestGoListModuleIgnoresPackagesInsideSkippedDirectoriesInCleanAndInstalledStates(t *testing.T) {
	root := newGitRepository(t, "node_modules/\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/root\n\ngo 1.25.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "public.go"), []byte("package root\n\nfunc Public() {}\n"), 0o600))

	clean, err := GoListModule(root)
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/root"}, packagePaths(clean))

	installedPackage := filepath.Join(root, "tools", "vscode-extension", "node_modules", "flatted", "golang", "pkg", "flatted")
	require.NoError(t, os.MkdirAll(installedPackage, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installedPackage, "flatted.go"), []byte("package flatted\n\nfunc FromJSON() {}\n"), 0o600))

	installed, err := GoListModule(root)
	require.NoError(t, err)
	require.Equal(t, packagePaths(clean), packagePaths(installed))
	require.Equal(t, []string{"example.test/root"}, PublicPackagesForModule(installed, "example.test/root"))
}

func packagePaths(packages []Package) []string {
	paths := make([]string, 0, len(packages))
	for _, pkg := range packages {
		paths = append(paths, pkg.ImportPath)
	}
	return paths
}

func TestPublicPackagesRejectsMainInternalAndTestOnlyPackages(t *testing.T) {
	packages := []Package{
		{ImportPath: modulePath + "/embedded", Name: "embedded", GoFiles: []string{"embedded.go"}},
		{ImportPath: modulePath + "/cmd/tool", Name: "main", GoFiles: []string{"main.go"}},
		{ImportPath: modulePath + "/internal/engine", Name: "engine", GoFiles: []string{"engine.go"}},
		{ImportPath: modulePath + "/tests", Name: "tests"},
	}
	require.Equal(t, []string{modulePath + "/embedded"}, PublicPackages(packages))
}

func TestPublicAPIDetectsExportsWithoutTrackingPrivateDetails(t *testing.T) {
	dir := t.TempDir()
	source := `package sample

type Public struct {
	Visible string
	hidden string
}
type private struct{ Leaked string }
func Exported(value string) error { return nil }
func hidden() {}
func (Public) Method(value int) {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o600))
	api, err := PublicAPI([]Package{{ImportPath: modulePath + "/sample", Name: "sample", Dir: dir, GoFiles: []string{"sample.go"}}})
	require.NoError(t, err)
	require.Equal(t, []string{
		modulePath + "/sample\tfunc Exported(value string) error",
		modulePath + "/sample\tmethod Public.Method(value int)",
		modulePath + "/sample\ttype Public struct{Visible string}",
	}, api)
	for _, declaration := range api {
		require.NotContains(t, declaration, "hidden")
		require.NotContains(t, declaration, "Leaked")
	}
}

func TestPublicAPIRejectsInternalPackageTypes(t *testing.T) {
	dir := t.TempDir()
	source := `package sample

import hidden "example.test/root/internal/hidden"

type Public struct{ Value hidden.Value }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o600))
	_, err := PublicAPIForModule([]Package{{ImportPath: "example.test/root/sample", Name: "sample", Dir: dir, GoFiles: []string{"sample.go"}}}, "example.test/root")
	require.EqualError(t, err, "public declaration Public exposes internal package example.test/root/internal/hidden")
}

func TestDependencyRulesDetectOnlySegmentBoundedEdges(t *testing.T) {
	rules := []DependencyRule{{From: modulePath + "/compiler/...", To: modulePath + "/runtime/...", Reason: "compiler must not depend on runtime"}}
	violations := CheckDependencyRules([]Package{
		{ImportPath: modulePath + "/compiler/check", Imports: []string{modulePath + "/runtime/engine", modulePath + "/runtimekit"}},
		{ImportPath: modulePath + "/compilerkit", Imports: []string{modulePath + "/runtime/engine"}},
	}, rules)
	require.Equal(t, []string{modulePath + "/compiler/check imports forbidden " + modulePath + "/runtime/engine (compiler must not depend on runtime)"}, violations)
}

func TestForbiddenProductionSymbolsRejectRemovedCompilerAndRuntimeAuthorities(t *testing.T) {
	dir := t.TempDir()
	source := `package fixture

type ExecutionRuntime struct{}
type CompiledUnit struct{}
type CompiledSpec struct{}
type CompileOptions struct { InspectSource func() }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(source), 0o600))
	violations, err := CheckForbiddenProductionSymbols([]Package{{ImportPath: modulePath + "/fixture", Dir: dir, GoFiles: []string{"fixture.go"}}})
	require.NoError(t, err)
	require.Equal(t, []string{
		modulePath + "/fixture declares forbidden CompileOptions.InspectSource",
		modulePath + "/fixture declares forbidden type CompiledSpec",
		modulePath + "/fixture declares forbidden type CompiledUnit",
		modulePath + "/fixture declares forbidden type ExecutionRuntime",
	}, violations)
}

func TestDeprecationDeadlinesHavePositiveAndNegativeCoverage(t *testing.T) {
	root := newGitRepository(t, "gen/\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "valid.go"), []byte("package fixture\n// Deprecated: use Current. Removal deadline: 2030-01-02.\nfunc Old() {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "missing.go"), []byte("package fixture\n// Deprecated: use Current.\nfunc Missing() {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "expired.go"), []byte("package fixture\n// Deprecated: use Current. Removal deadline: 2029-12-31.\nfunc Expired() {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "generated.pb.go"), []byte("package fixture\n// Deprecated: generated compatibility.\nfunc Generated() {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "literal.go"), []byte("package fixture\nconst diagnostic = `Deprecated: is only checked in comments`\n"), 0o600))
	violations, err := CheckDeprecations(root, time.Date(2030, 1, 1, 23, 0, 0, 0, time.FixedZone("test", 3600)))
	require.NoError(t, err)
	require.Equal(t, []string{
		"expired.go:2 deprecation expired on 2029-12-31",
		"missing.go:2 deprecated API has no Removal deadline: YYYY-MM-DD",
	}, violations)
}

func TestCanonicalExecutorDocsRejectsUnsupportedResolverClaims(t *testing.T) {
	violations := CheckCanonicalExecutorDocs("Production supports checked HTTP, gRPC, stream, Kafka, and OCI-resolved targets.")
	require.Equal(t, []string{"unsupported executor claim: supports checked HTTP, gRPC, stream, Kafka, and OCI-resolved targets"}, violations)
	require.Empty(t, CheckCanonicalExecutorDocs("Production supports the canonical HTTP executor target only."))
}

func TestCompatibilityDocsRequireV040IntroductionBoundary(t *testing.T) {
	valid := "Version 0.4.0 introduces the frozen v0.3 compatibility packages.\n" +
		"Published `v0.3.0` does not contain these paths.\n" +
		"just smoke-compat \"$ROOT_VERSION\"\n"
	require.Empty(t, CheckCompatibilityDocs(valid))
	require.Equal(t, []string{
		"missing compatibility release boundary: Published `v0.3.0` does not contain these paths.",
		"missing compatibility release boundary: Version 0.4.0 introduces the frozen v0.3 compatibility packages.",
		"missing compatibility release boundary: just smoke-compat \"$ROOT_VERSION\"",
	}, CheckCompatibilityDocs("A root tag such as v0.3.0 publishes all paths."))
}

func TestCompatibilityReleaseClaimsRejectPublishedV030PathParaphrases(t *testing.T) {
	for _, claim := range []string{
		"The v0.3.0 release publishes compat/v03/invocation.",
		"compat/v03/executorhttp is provided by v0.3.0.",
		"v0.3.0 includes compat/v03/embedded in its root module.",
		"The v0.3.0 tag ships compat/v03/invocation.",
		"Consumers of v0.3.0 can import compat/v03/invocation.",
		"compat/v03/executorhttp is available in v0.3.0.",
		"v0.3.0 comes with compat/v03/embedded.",
	} {
		t.Run(claim, func(t *testing.T) {
			require.Equal(t, []string{"incorrect published compatibility claim: v0.3.0 affirmatively claims a compat/v03 path"}, CheckCompatibilityReleaseClaims(claim))
		})
	}
}

func TestCompatibilityReleaseClaimsAllowExplicitV030Negation(t *testing.T) {
	for _, claim := range []string{
		"Published v0.3.0 does not contain compat/v03/invocation.",
		"The v0.3.0 release never provided compat/v03/executorhttp.",
		"v0.3.0 did not publish compat/v03/embedded.",
		"It is false that v0.3.0 includes compat/v03/invocation.",
		"compat/v03/executorhttp is not among the paths v0.3.0 provides.",
	} {
		t.Run(claim, func(t *testing.T) {
			require.Empty(t, CheckCompatibilityReleaseClaims(claim))
		})
	}
}

func TestBudgetCountsRejectStaleCurrentValues(t *testing.T) {
	budgets := `| Surface | Budget | Current | Definition |
| --- | ---: | ---: | --- |
| Product package domains | 18 | 18 | Product packages. |
| Immediate examples | 3 | 3 | Examples. |
| Visible Just recipes | 18 | 18 | Recipes. |
| Go modules | 1 | 1 | Root module. |`
	counts := map[string]int{
		"Product package domains": 18,
		"Immediate examples":      3,
		"Visible Just recipes":    18,
		"Go modules":              1,
	}

	require.Empty(t, CheckBudgetCounts(budgets, counts))
	require.Equal(t, []string{"budget \"Go modules\" Current=2; inventory=1"}, CheckBudgetCounts(strings.Replace(budgets, "| Go modules | 1 | 1 |", "| Go modules | 2 | 2 |", 1), counts))
}

func TestModulePathAndSurfaceInventoriesRejectUnexpectedFixtureGrowth(t *testing.T) {
	root := newGitRepository(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/root\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "examples", "new-example"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "examples", "new-example", "main.go"), []byte("package main\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "examples", "shared-assets"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "new-top-level"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new-top-level", "package.go"), []byte("package fixture\n"), 0o600))

	path, err := ModulePath(root)
	require.NoError(t, err)
	require.Equal(t, "example.test/root", path)
	examples, err := DiscoverExamples(root)
	require.NoError(t, err)
	require.Equal(t, []string{"new-example"}, examples)
	topLevel, err := TopLevelDirectories(root)
	require.NoError(t, err)
	require.Contains(t, topLevel, "new-top-level")

	require.Error(t, CompareInventory("modules", []string{"."}, []string{".", "compat"}))
	require.Error(t, CompareInventory("examples", nil, examples))
	require.Error(t, CompareInventory("top-level directories", nil, topLevel))
	packages := []Package{{ImportPath: "example.test/root/public", Name: "public", GoFiles: []string{"public.go"}}}
	require.Error(t, CompareInventory("public packages", nil, PublicPackagesForModule(packages, path)))
	apiDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(apiDir, "public.go"), []byte("package public\nfunc Added() {}\n"), 0o600))
	api, err := PublicAPIForModule([]Package{{ImportPath: "example.test/root/public", Name: "public", Dir: apiDir, GoFiles: []string{"public.go"}}}, path)
	require.NoError(t, err)
	require.Error(t, CompareInventory("public API", nil, api))
}

func TestSurfaceInventoriesIgnoreGitIgnoredOutputAndDetectUntrackedPackages(t *testing.T) {
	root := newGitRepository(t, "clients/\ngen/runtime/\nnode_modules/\nout/\nbuild/\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/root\n\ngo 1.25.0\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "stable"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "stable", "stable.go"), []byte("package stable\n"), 0o600))

	beforeModules, err := DiscoverModules(root)
	require.NoError(t, err)
	beforePackages, err := GoListModule(root)
	require.NoError(t, err)
	beforeTopLevel, err := TopLevelDirectories(root)
	require.NoError(t, err)

	for _, path := range []string{
		"clients/generated/client.go",
		"clients/generated/go.mod",
		"gen/runtime/generated.go",
		"gen/runtime/go.mod",
		"node_modules/library/index.js",
		"node_modules/library/go.mod",
		"out/build/result.go",
		"out/build/go.mod",
		"build/generated.go",
		"unignored-artifact/result.bin",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		data := []byte("generated\n")
		switch filepath.Base(path) {
		case "go.mod":
			data = []byte("module example.test/generated\n\ngo 1.25.0\n")
		default:
			if filepath.Ext(path) == ".go" {
				data = []byte("package generated\n")
			}
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, path), data, 0o600))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "states"), 0o755))

	afterModules, err := DiscoverModules(root)
	require.NoError(t, err)
	afterPackages, err := GoListModule(root)
	require.NoError(t, err)
	afterTopLevel, err := TopLevelDirectories(root)
	require.NoError(t, err)
	require.Equal(t, beforeModules, afterModules)
	require.Equal(t, packagePaths(beforePackages), packagePaths(afterPackages))
	require.Equal(t, beforeTopLevel, afterTopLevel)
	require.NotContains(t, afterTopLevel, "states")
	require.NotContains(t, afterTopLevel, "unignored-artifact")

	require.NoError(t, os.MkdirAll(filepath.Join(root, "newpackage"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "newpackage", "newpackage.go"), []byte("package newpackage\n"), 0o600))
	withNewPackage, err := TopLevelDirectories(root)
	require.NoError(t, err)
	require.Contains(t, withNewPackage, "newpackage")
	packagesWithNewPackage, err := GoListModule(root)
	require.NoError(t, err)
	require.Contains(t, packagePaths(packagesWithNewPackage), "example.test/root/newpackage")
	require.Error(t, CompareInventory("top-level directories", beforeTopLevel, withNewPackage))
}

func newGitRepository(t *testing.T, ignore string) string {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "init", "--quiet", root)
	require.NoError(t, command.Run())
	if ignore != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte(ignore), 0o600))
	}
	return root
}

func TestRecipeInventoryDetectsAdditionAndIgnoresPrivateHelpersAndComments(t *testing.T) {
	recipes := ParseRecipes("# fake:\nset shell := [\"bash\"]\nbuild target='all':\n\techo {{target}}\n[private]\n_test-helper:\n")
	require.Equal(t, []string{"build"}, recipes)
	err := CompareInventory("recipes", []string{"build"}, append(recipes, "new-visible-task"))
	require.EqualError(t, err, "recipes changed; removed=[] added=[new-visible-task] (update only with an intentional surface review)")
}
