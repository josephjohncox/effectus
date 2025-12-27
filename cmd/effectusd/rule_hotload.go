package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/lint"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
)

var positionPattern = regexp.MustCompile(`:(\d+):(\d+)`)

type ruleHotloadRequest struct {
	Files   []ruleHotloadFile `json:"files,omitempty"`
	Path    string            `json:"path,omitempty"`
	Content string            `json:"content,omitempty"`
	Format  string            `json:"format,omitempty"`
	Replace *bool             `json:"replace,omitempty"`
}

type ruleHotloadFile struct {
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Format  string `json:"format,omitempty"`
}

type ruleDiagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
}

type ruleCheckResponse struct {
	OK            bool             `json:"ok"`
	Applied       bool             `json:"applied,omitempty"`
	Rules         int              `json:"rules,omitempty"`
	Flows         int              `json:"flows,omitempty"`
	RequiredFacts []string         `json:"required_facts,omitempty"`
	RuleFiles     []string         `json:"rule_files,omitempty"`
	Diagnostics   []ruleDiagnostic `json:"diagnostics,omitempty"`
}

func (s *serverState) handleRuleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.rulesOn {
		writeJSONError(w, http.StatusForbidden, "rule hotload disabled")
		return
	}

	req, err := decodeRuleHotloadRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	result := s.evaluateRuleHotload(req, false)
	writeJSON(w, http.StatusOK, result)
}

func (s *serverState) handleRuleHotload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.rulesOn {
		writeJSONError(w, http.StatusForbidden, "rule hotload disabled")
		return
	}

	req, err := decodeRuleHotloadRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	result := s.evaluateRuleHotload(req, true)
	writeJSON(w, http.StatusOK, result)
}

func decodeRuleHotloadRequest(r *http.Request) (ruleHotloadRequest, error) {
	if r == nil || r.Body == nil {
		return ruleHotloadRequest{}, fmt.Errorf("missing request body")
	}
	var req ruleHotloadRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		return ruleHotloadRequest{}, fmt.Errorf("invalid JSON payload: %w", err)
	}
	return req, nil
}

func (s *serverState) evaluateRuleHotload(req ruleHotloadRequest, apply bool) ruleCheckResponse {
	files, replace, err := normalizeRuleHotloadRequest(req)
	if err != nil {
		return ruleCheckResponse{
			OK:          false,
			Diagnostics: []ruleDiagnostic{{Severity: lint.SeverityError, Message: err.Error(), Line: 1, Column: 1}},
		}
	}

	bundle := s.Bundle()
	if bundle == nil {
		return ruleCheckResponse{
			OK:          false,
			Diagnostics: []ruleDiagnostic{{Severity: lint.SeverityError, Message: "bundle not loaded", Line: 1, Column: 1}},
		}
	}

	sources, ruleFiles := s.mergeRuleSources(bundle, files, replace)
	prepared, cleanup, err := prepareRuleSources(sources)
	if err != nil {
		return ruleCheckResponse{
			OK:          false,
			Diagnostics: []ruleDiagnostic{{Severity: lint.SeverityError, Message: err.Error(), Line: 1, Column: 1}},
		}
	}
	defer cleanup()

	typeSystem := buildHotloadTypeSystem(s.typeSystem, bundle, s.verbReg)
	facts := newHotloadFacts(typeSystem)

	comp := compiler.NewCompiler()
	compTS := comp.GetTypeSystem()
	if compTS != nil {
		compTS.MergeTypeSystem(typeSystem)
	}

	issues := typecheckRuleSources(comp, facts, prepared, s.verbReg)
	diagnostics := issuesToDiagnostics(issues)
	if hasDiagnosticErrors(issues) {
		return ruleCheckResponse{OK: false, Diagnostics: diagnostics}
	}

	if !apply {
		return ruleCheckResponse{
			OK:          true,
			Diagnostics: diagnostics,
			RuleFiles:   ruleFiles,
		}
	}

	spec, err := comp.ParseAndCompileFiles(collectTempPaths(prepared), facts)
	if err != nil {
		issues = append(issues, issueFromError("compile", err))
		return ruleCheckResponse{OK: false, Diagnostics: issuesToDiagnostics(issues)}
	}

	next := *bundle
	next.ListSpec = extractListSpec(spec)
	next.FlowSpec = extractFlowSpec(spec)
	next.Rules = unified.SummarizeRules(next.ListSpec)
	next.Flows = unified.SummarizeFlows(next.FlowSpec)
	next.RequiredFacts = spec.RequiredFacts()
	next.RuleSources = sources
	next.RuleFiles = ruleFiles

	s.SetBundle(&next)

	return ruleCheckResponse{
		OK:            true,
		Applied:       true,
		Rules:         len(next.Rules),
		Flows:         len(next.Flows),
		RequiredFacts: next.RequiredFacts,
		RuleFiles:     ruleFiles,
		Diagnostics:   diagnostics,
	}
}

type preparedRuleSource struct {
	DisplayPath string
	TempPath    string
	Format      string
}

func normalizeRuleHotloadRequest(req ruleHotloadRequest) ([]ruleHotloadFile, bool, error) {
	files := req.Files
	if len(files) == 0 && strings.TrimSpace(req.Content) != "" {
		files = []ruleHotloadFile{{
			Path:    req.Path,
			Content: req.Content,
			Format:  req.Format,
		}}
	}
	if len(files) == 0 {
		return nil, false, fmt.Errorf("no rule sources provided")
	}

	normalized := make([]ruleHotloadFile, 0, len(files))
	for idx, file := range files {
		content := strings.TrimSpace(file.Content)
		if content == "" {
			return nil, false, fmt.Errorf("rule content is empty")
		}
		path := strings.TrimSpace(file.Path)
		format := normalizeRuleFormat(file.Format, path)
		if format == "" {
			format = "eff"
		}
		if format != "eff" && format != "effx" {
			return nil, false, fmt.Errorf("unsupported rule format %q", format)
		}
		if path == "" {
			path = fmt.Sprintf("rules/hotload-%d.%s", idx+1, format)
		}
		normalized = append(normalized, ruleHotloadFile{
			Path:    path,
			Content: content,
			Format:  format,
		})
	}

	replace := false
	if req.Replace != nil {
		replace = *req.Replace
	}

	return normalized, replace, nil
}

func normalizeRuleFormat(format string, path string) string {
	format = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(format), "."))
	if format != "" {
		return format
	}
	if path == "" {
		return ""
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".effx":
		return "effx"
	case ".eff":
		return "eff"
	default:
		return ""
	}
}

func (s *serverState) mergeRuleSources(bundle *unified.Bundle, incoming []ruleHotloadFile, replace bool) ([]unified.RuleSource, []string) {
	merged := make(map[string]unified.RuleSource)
	if !replace && bundle != nil {
		for idx, source := range bundle.RuleSources {
			path := strings.TrimSpace(source.Path)
			if path == "" {
				format := normalizeRuleFormat(source.Format, "")
				if format == "" {
					format = "eff"
				}
				path = fmt.Sprintf("rules/bundle-%d.%s", idx+1, format)
			}
			format := normalizeRuleFormat(source.Format, path)
			if format == "" {
				format = "eff"
			}
			merged[path] = unified.RuleSource{
				Path:    path,
				Format:  format,
				Content: source.Content,
			}
		}
	}

	for _, file := range incoming {
		merged[file.Path] = unified.RuleSource{
			Path:    file.Path,
			Format:  normalizeRuleFormat(file.Format, file.Path),
			Content: file.Content,
		}
	}

	paths := make([]string, 0, len(merged))
	for path := range merged {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	sources := make([]unified.RuleSource, 0, len(paths))
	for _, path := range paths {
		source := merged[path]
		if source.Format == "" {
			source.Format = normalizeRuleFormat("", source.Path)
		}
		if source.Format == "" {
			source.Format = "eff"
		}
		sources = append(sources, source)
	}

	return sources, paths
}

func prepareRuleSources(sources []unified.RuleSource) ([]preparedRuleSource, func(), error) {
	if len(sources) == 0 {
		return nil, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "effectus-hotload-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	prepared := make([]preparedRuleSource, 0, len(sources))
	for idx, source := range sources {
		format := normalizeRuleFormat(source.Format, source.Path)
		ext := ".eff"
		if format == "effx" {
			ext = ".effx"
		}
		base := filepath.Base(source.Path)
		if base == "" || base == "." || base == string(filepath.Separator) {
			base = fmt.Sprintf("rule-%d%s", idx+1, ext)
		}
		if filepath.Ext(base) != ext {
			base = strings.TrimSuffix(base, filepath.Ext(base)) + ext
		}
		tempPath := filepath.Join(dir, base)
		if err := os.WriteFile(tempPath, []byte(source.Content), 0600); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("write temp rule file: %w", err)
		}
		prepared = append(prepared, preparedRuleSource{
			DisplayPath: source.Path,
			TempPath:    tempPath,
			Format:      format,
		})
	}

	return prepared, cleanup, nil
}

func collectTempPaths(prepared []preparedRuleSource) []string {
	paths := make([]string, 0, len(prepared))
	for _, file := range prepared {
		paths = append(paths, file.TempPath)
	}
	return paths
}

func typecheckRuleSources(comp *compiler.Compiler, facts effectus.Facts, prepared []preparedRuleSource, verbs *verb.Registry) []lint.Issue {
	if comp == nil {
		return []lint.Issue{{Severity: lint.SeverityError, Message: "compiler not available", Pos: lexer.Position{Line: 1, Column: 1}}}
	}
	issues := make([]lint.Issue, 0)
	options := lint.DefaultOptions()
	var lookup lint.VerbLookup
	if verbs != nil {
		lookup = verbs
	}
	for _, file := range prepared {
		parsed, err := comp.ParseAndTypeCheck(file.TempPath, facts)
		if err != nil {
			issues = append(issues, issueFromError(file.DisplayPath, err))
			continue
		}
		issues = append(issues, lint.LintFileWithOptions(parsed, file.DisplayPath, lookup, options)...)
	}
	return issues
}

func issuesToDiagnostics(issues []lint.Issue) []ruleDiagnostic {
	if len(issues) == 0 {
		return nil
	}
	out := make([]ruleDiagnostic, 0, len(issues))
	for _, issue := range issues {
		line := issue.Pos.Line
		col := issue.Pos.Column
		if line <= 0 {
			line = 1
		}
		if col <= 0 {
			col = 1
		}
		out = append(out, ruleDiagnostic{
			File:     issue.File,
			Line:     line,
			Column:   col,
			Severity: issue.Severity,
			Code:     issue.Code,
			Message:  issue.Message,
		})
	}
	return out
}

func hasDiagnosticErrors(issues []lint.Issue) bool {
	for _, issue := range issues {
		if strings.ToLower(issue.Severity) == lint.SeverityError {
			return true
		}
	}
	return false
}

func issueFromError(file string, err error) lint.Issue {
	pos := positionFromError(err)
	return lint.Issue{
		File:     file,
		Pos:      pos,
		Severity: lint.SeverityError,
		Code:     "typecheck",
		Message:  err.Error(),
	}
}

func positionFromError(err error) lexer.Position {
	if err == nil {
		return lexer.Position{}
	}
	matches := positionPattern.FindAllStringSubmatch(err.Error(), -1)
	if len(matches) == 0 {
		return lexer.Position{}
	}
	last := matches[len(matches)-1]
	if len(last) < 3 {
		return lexer.Position{}
	}
	line := parseIssueInt(last[1])
	col := parseIssueInt(last[2])
	return lexer.Position{Line: line, Column: col}
}

func parseIssueInt(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	out, _ := strconv.Atoi(value)
	return out
}

func buildHotloadTypeSystem(runtime *types.TypeSystem, bundle *unified.Bundle, verbs *verb.Registry) *types.TypeSystem {
	ts := types.NewTypeSystem()

	if runtime != nil {
		for _, path := range runtime.GetAllFactPaths() {
			if base, version, ok := splitVersionedPath(path); ok {
				if typ, err := runtime.GetFactTypeVersion(base, version); err == nil && typ != nil {
					ts.RegisterFactTypeVersion(base, version, typ.Clone(), false)
					continue
				}
			}
			if typ, err := runtime.GetFactType(path); err == nil && typ != nil {
				ts.RegisterFactType(path, typ.Clone())
			}
		}
		for _, name := range runtime.GetAllVerbNames() {
			spec, err := runtime.GetVerbSpec(name)
			if err != nil || spec == nil {
				continue
			}
			_ = ts.RegisterVerb(spec.Name, spec.ArgTypes, spec.ReturnType, spec.RequiredArgs)
		}
		for _, spec := range runtime.GetFunctionSpecs() {
			if spec == nil {
				continue
			}
			ts.RegisterFunctionSpec(spec)
		}
	}

	if bundle != nil {
		for _, fact := range bundle.FactTypes {
			registerFactSummary(ts, fact)
		}
		for _, summary := range bundle.VerbSpecs {
			registerVerbSummary(ts, summary)
		}
	}

	if verbs != nil {
		for _, spec := range verbs.GetAllVerbs() {
			registerVerbSpec(ts, spec)
		}
	}

	return ts
}

func registerFactSummary(ts *types.TypeSystem, summary unified.FactTypeSummary) {
	if ts == nil {
		return
	}
	typ, _ := types.ParseTypeName(summary.Type)
	if typ == nil {
		typ = types.NewAnyType()
	}
	if base, version, ok := splitVersionedPath(summary.Path); ok {
		ts.RegisterFactTypeVersion(base, version, typ, false)
		return
	}
	ts.RegisterFactType(summary.Path, typ)
}

func registerVerbSummary(ts *types.TypeSystem, summary unified.VerbSpecSummary) {
	if ts == nil || summary.Name == "" {
		return
	}
	argTypes := make(map[string]*types.Type, len(summary.ArgTypes))
	for name, typeName := range summary.ArgTypes {
		argType, _ := types.ParseTypeName(typeName)
		if argType == nil {
			argType = types.NewAnyType()
		}
		argTypes[name] = argType
	}
	retType, _ := types.ParseTypeName(summary.ReturnType)
	if retType == nil {
		retType = types.NewAnyType()
	}
	_ = ts.RegisterVerb(summary.Name, argTypes, retType, summary.RequiredArgs)
}

func registerVerbSpec(ts *types.TypeSystem, spec *verb.Spec) {
	if ts == nil || spec == nil {
		return
	}
	argTypes := make(map[string]*types.Type, len(spec.ArgTypes))
	for name, typeName := range spec.ArgTypes {
		argType, _ := types.ParseTypeName(typeName)
		if argType == nil {
			argType = types.NewAnyType()
		}
		argTypes[name] = argType
	}
	retType, _ := types.ParseTypeName(spec.ReturnType)
	if retType == nil {
		retType = types.NewAnyType()
	}
	_ = ts.RegisterVerb(spec.Name, argTypes, retType, spec.RequiredArgs)
}

type hotloadSchema struct {
	typeSystem *types.TypeSystem
}

func (s *hotloadSchema) ValidatePath(path string) bool {
	if s == nil || s.typeSystem == nil || strings.TrimSpace(path) == "" {
		return false
	}
	_, err := s.typeSystem.GetFactType(path)
	return err == nil
}

type hotloadFacts struct {
	factRegistry *pathutil.Registry
	schema       *hotloadSchema
}

func newHotloadFacts(ts *types.TypeSystem) *hotloadFacts {
	registry := pathutil.NewRegistry()
	registry.Register("", pathutil.NewRegistryFactProviderFromMap(map[string]interface{}{}))
	return &hotloadFacts{
		factRegistry: registry,
		schema:       &hotloadSchema{typeSystem: ts},
	}
}

func (f *hotloadFacts) Get(path string) (interface{}, bool) {
	return f.factRegistry.Get(path)
}

func (f *hotloadFacts) Schema() effectus.SchemaInfo {
	return f.schema
}

func splitVersionedPath(path string) (string, string, bool) {
	idx := strings.LastIndex(path, "@")
	if idx == -1 {
		return "", "", false
	}
	base := strings.TrimSpace(path[:idx])
	version := strings.TrimSpace(path[idx+1:])
	if base == "" || version == "" {
		return "", "", false
	}
	return base, version, true
}

func extractListSpec(spec effectus.Spec) *list.Spec {
	if spec == nil {
		return nil
	}
	type specWithListField interface {
		ListSpec() *list.Spec
	}
	if s, ok := spec.(specWithListField); ok {
		return s.ListSpec()
	}
	return nil
}

func extractFlowSpec(spec effectus.Spec) *flow.Spec {
	if spec == nil {
		return nil
	}
	type specWithFlowField interface {
		FlowSpec() *flow.Spec
	}
	if s, ok := spec.(specWithFlowField); ok {
		return s.FlowSpec()
	}
	return nil
}
