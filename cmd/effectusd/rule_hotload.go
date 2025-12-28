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
	"time"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/lint"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema"
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
	Confirm *bool             `json:"confirm,omitempty"`
	Canary  *hotloadCanary    `json:"canary,omitempty"`
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
	Confirmed     bool             `json:"confirmed,omitempty"`
	HealthOK      bool             `json:"health_ok,omitempty"`
	Rules         int              `json:"rules,omitempty"`
	Flows         int              `json:"flows,omitempty"`
	RequiredFacts []string         `json:"required_facts,omitempty"`
	RuleFiles     []string         `json:"rule_files,omitempty"`
	Diagnostics   []ruleDiagnostic `json:"diagnostics,omitempty"`
	SourceDiff    []ruleSourceDiff `json:"source_diff,omitempty"`
	Canary        *canaryResult    `json:"canary,omitempty"`
	HealthErrors  []string         `json:"health_errors,omitempty"`
}

type hotloadCanary struct {
	Universe  string                 `json:"universe,omitempty"`
	Facts     map[string]interface{} `json:"facts,omitempty"`
	Mode      string                 `json:"mode,omitempty"`
	UseStored bool                   `json:"use_stored,omitempty"`
}

type canaryResult struct {
	Mode           string         `json:"mode"`
	RulesChanged   []string       `json:"rules_changed,omitempty"`
	FlowsChanged   []string       `json:"flows_changed,omitempty"`
	CurrentSummary dryRunSummary  `json:"current_summary"`
	StagedSummary  dryRunSummary  `json:"staged_summary"`
	Errors         []string       `json:"errors,omitempty"`
	Universe       string         `json:"universe,omitempty"`
	Facts          map[string]int `json:"facts,omitempty"`
}

type ruleSourceDiff struct {
	Path   string `json:"path"`
	Format string `json:"format,omitempty"`
	Change string `json:"change"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
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

	if apply {
		recordHotloadAttempt()
	}

	sources, ruleFiles := s.mergeRuleSources(bundle, files, replace)
	sourceDiff := diffRuleSources(bundle.RuleSources, sources)
	prepared, cleanup, err := prepareRuleSources(sources)
	if err != nil {
		return ruleCheckResponse{
			OK:          false,
			Diagnostics: []ruleDiagnostic{{Severity: lint.SeverityError, Message: err.Error(), Line: 1, Column: 1}},
			SourceDiff:  sourceDiff,
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

	typecheckStart := time.Now()
	issues := typecheckRuleSources(comp, facts, prepared, s.verbReg)
	observeTypecheckDuration(time.Since(typecheckStart))
	diagnostics := issuesToDiagnostics(issues)
	if hasDiagnosticErrors(issues) {
		if apply {
			recordHotloadFailure()
		}
		return ruleCheckResponse{
			OK:          false,
			Diagnostics: diagnostics,
			SourceDiff:  sourceDiff,
		}
	}

	var staged *unified.Bundle
	var canary *canaryResult
	var healthErrors []string

	needsCompile := apply || req.Canary != nil
	if needsCompile {
		spec, err := comp.ParseAndCompileFiles(collectTempPaths(prepared), facts)
		if err != nil {
			issues = append(issues, issueFromError("compile", err))
			if apply {
				recordHotloadFailure()
			}
			return ruleCheckResponse{OK: false, Diagnostics: issuesToDiagnostics(issues), SourceDiff: sourceDiff}
		}
		recordRuleCompile()

		next := *bundle
		next.ListSpec = extractListSpec(spec)
		next.FlowSpec = extractFlowSpec(spec)
		next.Rules = unified.SummarizeRules(next.ListSpec)
		next.Flows = unified.SummarizeFlows(next.FlowSpec)
		next.RequiredFacts = spec.RequiredFacts()
		next.RuleSources = sources
		next.RuleFiles = ruleFiles
		staged = &next

		if req.Canary != nil {
			result, errors := s.runCanary(bundle, staged, req.Canary)
			canary = result
			healthErrors = errors
		}
	}

	if !apply {
		return ruleCheckResponse{
			OK:          true,
			Diagnostics: diagnostics,
			RuleFiles:   ruleFiles,
			SourceDiff:  sourceDiff,
			Canary:      canary,
			HealthErrors: func() []string {
				if len(healthErrors) == 0 {
					return nil
				}
				return healthErrors
			}(),
			HealthOK: len(healthErrors) == 0,
		}
	}

	if staged == nil {
		recordHotloadFailure()
		return ruleCheckResponse{
			OK:          false,
			Diagnostics: diagnostics,
			SourceDiff:  sourceDiff,
		}
	}

	confirm := true
	if req.Confirm != nil {
		confirm = *req.Confirm
	}

	if !confirm {
		return ruleCheckResponse{
			OK:          true,
			Applied:     false,
			Confirmed:   false,
			HealthOK:    true,
			Rules:       len(staged.Rules),
			Flows:       len(staged.Flows),
			RuleFiles:   ruleFiles,
			Diagnostics: diagnostics,
			SourceDiff:  sourceDiff,
			Canary:      canary,
		}
	}

	previous := bundle
	s.SetBundle(staged)

	if len(healthErrors) > 0 {
		s.SetBundle(previous)
		recordHotloadFailure()
		return ruleCheckResponse{
			OK:           false,
			Applied:      false,
			Confirmed:    false,
			HealthOK:     false,
			HealthErrors: healthErrors,
			Diagnostics:  diagnostics,
			SourceDiff:   sourceDiff,
			Canary:       canary,
		}
	}

	return ruleCheckResponse{
		OK:            true,
		Applied:       true,
		Confirmed:     true,
		HealthOK:      true,
		Rules:         len(staged.Rules),
		Flows:         len(staged.Flows),
		RequiredFacts: staged.RequiredFacts,
		RuleFiles:     ruleFiles,
		Diagnostics:   diagnostics,
		SourceDiff:    sourceDiff,
		Canary:        canary,
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

func diffRuleSources(before []unified.RuleSource, after []unified.RuleSource) []ruleSourceDiff {
	if len(before) == 0 && len(after) == 0 {
		return nil
	}
	beforeMap := make(map[string]unified.RuleSource)
	for _, source := range before {
		path := strings.TrimSpace(source.Path)
		if path == "" {
			continue
		}
		beforeMap[path] = source
	}
	afterMap := make(map[string]unified.RuleSource)
	for _, source := range after {
		path := strings.TrimSpace(source.Path)
		if path == "" {
			continue
		}
		afterMap[path] = source
	}

	paths := make([]string, 0, len(beforeMap)+len(afterMap))
	seen := make(map[string]struct{})
	for path := range beforeMap {
		paths = append(paths, path)
		seen[path] = struct{}{}
	}
	for path := range afterMap {
		if _, ok := seen[path]; !ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	diff := make([]ruleSourceDiff, 0)
	for _, path := range paths {
		beforeSource, beforeOK := beforeMap[path]
		afterSource, afterOK := afterMap[path]
		switch {
		case !beforeOK && afterOK:
			diff = append(diff, ruleSourceDiff{
				Path:   path,
				Format: normalizeRuleFormat(afterSource.Format, path),
				Change: "added",
				After:  afterSource.Content,
			})
		case beforeOK && !afterOK:
			diff = append(diff, ruleSourceDiff{
				Path:   path,
				Format: normalizeRuleFormat(beforeSource.Format, path),
				Change: "removed",
				Before: beforeSource.Content,
			})
		case beforeOK && afterOK:
			beforeContent := strings.TrimSpace(beforeSource.Content)
			afterContent := strings.TrimSpace(afterSource.Content)
			if beforeContent == afterContent {
				continue
			}
			diff = append(diff, ruleSourceDiff{
				Path:   path,
				Format: normalizeRuleFormat(afterSource.Format, path),
				Change: "modified",
				Before: beforeSource.Content,
				After:  afterSource.Content,
			})
		}
	}

	if len(diff) == 0 {
		return nil
	}
	return diff
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

func (s *serverState) runCanary(current *unified.Bundle, staged *unified.Bundle, canary *hotloadCanary) (*canaryResult, []string) {
	if canary == nil {
		return nil, nil
	}
	facts, universe, mode, err := s.resolveCanaryFacts(canary)
	if err != nil {
		return &canaryResult{Mode: mode, Universe: universe, Errors: []string{err.Error()}}, []string{err.Error()}
	}

	currentRun := buildDryRunFromBundle(current, facts, mode, universe)
	stagedRun := buildDryRunFromBundle(staged, facts, mode, universe)
	rulesChanged, flowsChanged := diffDryRuns(currentRun, stagedRun)
	errors := collectDryRunErrors(stagedRun)

	result := &canaryResult{
		Mode:           mode,
		RulesChanged:   rulesChanged,
		FlowsChanged:   flowsChanged,
		CurrentSummary: currentRun.Summary,
		StagedSummary:  stagedRun.Summary,
		Errors:         errors,
		Universe:       universe,
		Facts:          stagedRun.Facts,
	}

	return result, errors
}

func (s *serverState) resolveCanaryFacts(canary *hotloadCanary) (map[string]interface{}, string, string, error) {
	if canary == nil {
		return nil, "", "", fmt.Errorf("canary not provided")
	}
	universe := strings.TrimSpace(canary.Universe)
	if universe == "" {
		universe = "default"
	}
	mode := strings.ToLower(strings.TrimSpace(canary.Mode))
	if mode == "" {
		mode = "list"
	}
	facts := canary.Facts
	if len(facts) == 0 && canary.UseStored && s.factStore != nil {
		if snapshot, ok := s.factStore.Snapshot(universe); ok {
			facts = snapshot
		}
	}
	if len(facts) == 0 {
		return nil, universe, mode, fmt.Errorf("canary facts are required")
	}
	return facts, universe, mode, nil
}

func buildDryRunFromBundle(bundle *unified.Bundle, facts map[string]interface{}, mode string, universe string) dryRunResponse {
	resp := dryRunResponse{
		Universe: universe,
		Mode:     mode,
		Facts:    map[string]int{"namespaces": len(facts)},
	}
	if bundle == nil {
		resp.Errors = []string{"bundle not loaded"}
		return resp
	}

	registry := schema.NewRegistry()
	registry.LoadFromMap(facts)

	if mode == "list" || mode == "both" {
		rules := bundle.Rules
		if len(rules) == 0 && bundle.ListSpec != nil {
			rules = unified.SummarizeRules(bundle.ListSpec)
		}
		evaluated, matched := evaluateRules(rules, registry)
		resp.Rules = evaluated
		resp.Summary.RulesMatched = matched
		resp.Summary.RulesTotal = len(evaluated)
	}
	if mode == "flow" || mode == "both" {
		flows := bundle.Flows
		if len(flows) == 0 && bundle.FlowSpec != nil {
			flows = unified.SummarizeFlows(bundle.FlowSpec)
		}
		evaluated, matched := evaluateFlows(flows, registry)
		resp.Flows = evaluated
		resp.Summary.FlowsMatched = matched
		resp.Summary.FlowsTotal = len(evaluated)
	}

	return resp
}

func diffDryRuns(current dryRunResponse, staged dryRunResponse) ([]string, []string) {
	rulesChanged := make([]string, 0)
	flowsChanged := make([]string, 0)

	currentRules := make(map[string]string)
	for _, rule := range current.Rules {
		currentRules[rule.Name] = ruleSignature(rule)
	}
	stagedRules := make(map[string]string)
	for _, rule := range staged.Rules {
		stagedRules[rule.Name] = ruleSignature(rule)
	}
	for name, signature := range stagedRules {
		if currentRules[name] != signature {
			rulesChanged = append(rulesChanged, name)
		}
	}
	for name := range currentRules {
		if _, ok := stagedRules[name]; !ok {
			rulesChanged = append(rulesChanged, name)
		}
	}
	sort.Strings(rulesChanged)

	currentFlows := make(map[string]string)
	for _, flow := range current.Flows {
		currentFlows[flow.Name] = flowSignature(flow)
	}
	stagedFlows := make(map[string]string)
	for _, flow := range staged.Flows {
		stagedFlows[flow.Name] = flowSignature(flow)
	}
	for name, signature := range stagedFlows {
		if currentFlows[name] != signature {
			flowsChanged = append(flowsChanged, name)
		}
	}
	for name := range currentFlows {
		if _, ok := stagedFlows[name]; !ok {
			flowsChanged = append(flowsChanged, name)
		}
	}
	sort.Strings(flowsChanged)

	return rulesChanged, flowsChanged
}

func ruleSignature(rule dryRunRule) string {
	payload := struct {
		Matched bool         `json:"matched"`
		Effects []effectInfo `json:"effects"`
	}{
		Matched: rule.Matched,
		Effects: rule.Effects,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func flowSignature(flow dryRunFlow) string {
	payload := struct {
		Matched bool     `json:"matched"`
		Verbs   []string `json:"verbs"`
	}{
		Matched: flow.Matched,
		Verbs:   flow.Verbs,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func collectDryRunErrors(resp dryRunResponse) []string {
	errors := append([]string(nil), resp.Errors...)
	for _, rule := range resp.Rules {
		for _, pred := range rule.Predicates {
			if pred.Error != "" {
				errors = append(errors, pred.Error)
			}
		}
	}
	for _, flow := range resp.Flows {
		for _, pred := range flow.Predicates {
			if pred.Error != "" {
				errors = append(errors, pred.Error)
			}
		}
	}
	if len(errors) == 0 {
		return nil
	}
	return errors
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
