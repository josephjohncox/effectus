package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/lint"
	"github.com/effectus/effectus-go/schema/types"
)

type lspServer struct {
	reader      *bufio.Reader
	writer      io.Writer
	documents   map[string]string
	schemaFiles []string
	verbSchemas []string
	unsafeMode  lint.UnsafeMode
	verbMode    lint.VerbMode
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newLSPServer(reader io.Reader, writer io.Writer) *lspServer {
	return &lspServer{
		reader:    bufio.NewReader(reader),
		writer:    writer,
		documents: make(map[string]string),
	}
}

func newLSPCommand() *Command {
	lspCmd := &Command{
		Name:        "lsp",
		Description: "Start the Language Server Protocol (LSP) server",
		FlagSet:     flag.NewFlagSet("lsp", flag.ExitOnError),
	}

	lspCmd.Run = func() error {
		server := newLSPServer(os.Stdin, os.Stdout)
		return server.Run()
	}

	return lspCmd
}

func (s *lspServer) Run() error {
	for {
		payload, err := readLSPMessage(s.reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		var msg rpcMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}

		switch msg.Method {
		case "initialize":
			s.initialize(msg)
		case "initialized":
			// no-op
		case "shutdown":
			s.respond(msg.ID, nil)
		case "exit":
			return nil
		case "textDocument/didOpen":
			s.onDidOpen(msg)
		case "textDocument/didChange":
			s.onDidChange(msg)
		case "textDocument/didSave":
			s.onDidSave(msg)
		case "textDocument/completion":
			s.onCompletion(msg)
		case "textDocument/definition":
			s.onDefinition(msg)
		default:
			if len(msg.ID) > 0 {
				s.respondError(msg.ID, -32601, "method not found")
			}
		}
	}
}

func (s *lspServer) initialize(msg rpcMessage) {
	var params map[string]interface{}
	if err := json.Unmarshal(msg.Params, &params); err == nil {
		if options, ok := params["initializationOptions"].(map[string]interface{}); ok {
			s.schemaFiles = resolveSchemaFiles(parsePathOption(options["schemaPath"]))
			s.verbSchemas = resolveSchemaFiles(parsePathOption(options["verbSchemaPath"]))
			if rawMode, ok := options["unsafeMode"].(string); ok {
				if parsed, err := lint.ParseUnsafeMode(rawMode); err == nil {
					s.unsafeMode = parsed
				}
			}
			if rawVerbMode, ok := options["verbMode"].(string); ok {
				if parsed, err := lint.ParseVerbMode(rawVerbMode); err == nil {
					s.verbMode = parsed
				}
			}
		}
	}

	// Fall back to environment variables for CLI usage.
	if len(s.schemaFiles) == 0 {
		s.schemaFiles = resolveSchemaFiles(splitCommaList(os.Getenv("EFFECTUS_SCHEMA_PATH")))
	}
	if len(s.verbSchemas) == 0 {
		s.verbSchemas = resolveSchemaFiles(splitCommaList(os.Getenv("EFFECTUS_VERB_SPECS")))
	}
	if s.unsafeMode == "" {
		if envMode := os.Getenv("EFFECTUS_UNSAFE_MODE"); envMode != "" {
			if parsed, err := lint.ParseUnsafeMode(envMode); err == nil {
				s.unsafeMode = parsed
			}
		}
	}
	if s.unsafeMode == "" {
		s.unsafeMode = lint.UnsafeWarn
	}
	if s.verbMode == "" {
		if envMode := os.Getenv("EFFECTUS_VERB_LINT"); envMode != "" {
			if parsed, err := lint.ParseVerbMode(envMode); err == nil {
				s.verbMode = parsed
			}
		}
	}
	if s.verbMode == "" {
		s.verbMode = lint.VerbError
	}

	capabilities := map[string]interface{}{
		"textDocumentSync": 1,
		"completionProvider": map[string]interface{}{
			"resolveProvider": false,
		},
		"definitionProvider": true,
	}

	result := map[string]interface{}{"capabilities": capabilities}
	s.respond(msg.ID, result)
}

func (s *lspServer) onDidOpen(msg rpcMessage) {
	var params struct {
		TextDocument struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"textDocument"`
	}

	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	s.documents[params.TextDocument.URI] = params.TextDocument.Text
	s.publishDiagnostics(params.TextDocument.URI, params.TextDocument.Text)
}

func (s *lspServer) onDidChange(msg rpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}

	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	if len(params.ContentChanges) == 0 {
		return
	}

	text := params.ContentChanges[len(params.ContentChanges)-1].Text
	s.documents[params.TextDocument.URI] = text
	s.publishDiagnostics(params.TextDocument.URI, text)
}

func (s *lspServer) onDidSave(msg rpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}

	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	text, ok := s.documents[params.TextDocument.URI]
	if !ok {
		path := uriToPath(params.TextDocument.URI)
		if path != "" {
			data, err := os.ReadFile(path)
			if err == nil {
				text = string(data)
			}
		}
	}

	if text != "" {
		s.publishDiagnostics(params.TextDocument.URI, text)
	}
}

func (s *lspServer) onCompletion(msg rpcMessage) {
	items := s.buildCompletionItems()
	s.respond(msg.ID, items)
}

func (s *lspServer) onDefinition(msg rpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
	}

	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.respond(msg.ID, nil)
		return
	}

	text := s.documents[params.TextDocument.URI]
	if text == "" {
		path := uriToPath(params.TextDocument.URI)
		data, err := os.ReadFile(path)
		if err == nil {
			text = string(data)
		}
	}

	word := wordAtPosition(text, params.Position.Line, params.Position.Character)
	if word == "" {
		s.respond(msg.ID, nil)
		return
	}

	locations := s.definitionLocations(word)
	if len(locations) == 0 {
		s.respond(msg.ID, nil)
		return
	}
	s.respond(msg.ID, locations)
}

func (s *lspServer) publishDiagnostics(uri string, text string) {
	issues := s.checkText(uri, text)
	diagnostics := issuesToLSPDiagnostics(issues)

	params := map[string]interface{}{
		"uri":         uri,
		"diagnostics": diagnostics,
	}

	s.notify("textDocument/publishDiagnostics", params)
}

func (s *lspServer) checkText(uri string, text string) []lint.Issue {
	path := uriToPath(uri)
	ext := filepath.Ext(path)
	if ext == "" {
		ext = ".eff"
	}

	tmpFile, err := os.CreateTemp("", "effectus-lsp-*"+ext)
	if err != nil {
		return []lint.Issue{issueFromError(path, err)}
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(text); err != nil {
		_ = tmpFile.Close()
		return []lint.Issue{issueFromError(path, err)}
	}
	_ = tmpFile.Close()

	comp := compiler.NewCompiler()
	if len(s.verbSchemas) > 0 {
		for _, file := range s.verbSchemas {
			_ = comp.LoadVerbSpecs(file)
		}
	}

	schemaList := strings.Join(s.schemaFiles, ",")
	facts, typeSystem := createEmptyFacts(schemaList, false)
	compTS := comp.GetTypeSystem()
	compTS.MergeTypeSystem(typeSystem)

	parsed, err := comp.ParseFile(tmpFile.Name())
	if err != nil {
		return []lint.Issue{issueFromError(path, err)}
	}

	if len(s.schemaFiles) > 0 {
		if err := compTS.TypeCheckFile(parsed, facts); err != nil {
			return []lint.Issue{issueFromError(path, err)}
		}
	}

	registry := loadVerbRegistry(s.verbSchemas, false)
	return lint.LintFileWithOptions(parsed, path, registry, lint.LintOptions{
		UnsafeMode: s.unsafeMode,
		VerbMode:   s.verbMode,
	})
}

type completionItem struct {
	Label  string `json:"label"`
	Kind   int    `json:"kind,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func (s *lspServer) buildCompletionItems() []completionItem {
	items := make([]completionItem, 0)

	for _, fact := range s.loadKnownFacts() {
		items = append(items, completionItem{
			Label:  fact,
			Kind:   5, // Field
			Detail: "fact",
		})
	}

	for _, verbName := range s.loadKnownVerbs() {
		items = append(items, completionItem{
			Label:  verbName,
			Kind:   3, // Function
			Detail: "verb",
		})
	}

	return items
}

func (s *lspServer) loadKnownFacts() []string {
	if len(s.schemaFiles) == 0 {
		return nil
	}

	ts := types.NewTypeSystem()
	for _, file := range s.schemaFiles {
		if err := ts.LoadSchemaFile(file); err != nil {
			_ = ts.LoadJSONSchemaFile(file)
		}
	}

	return ts.GetAllFactPaths()
}

func (s *lspServer) loadKnownVerbs() []string {
	registry := loadVerbRegistry(s.verbSchemas, false)
	if registry == nil {
		return nil
	}

	verbs := registry.GetAllVerbs()
	names := make([]string, 0, len(verbs))
	for _, spec := range verbs {
		if spec != nil {
			names = append(names, spec.Name)
		}
	}
	sort.Strings(names)
	return names
}

type lspLocation struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

func (s *lspServer) definitionLocations(word string) []lspLocation {
	locations := make([]lspLocation, 0)

	if strings.Contains(word, ".") || strings.Contains(word, "[") {
		locations = append(locations, findFactLocations(word, s.schemaFiles)...)
	}

	locations = append(locations, findVerbLocations(word, s.verbSchemas)...)
	return locations
}

func wordAtPosition(text string, line, character int) string {
	if text == "" || line < 0 || character < 0 {
		return ""
	}

	lines := strings.Split(text, "\n")
	if line >= len(lines) {
		return ""
	}

	runes := []rune(lines[line])
	if character > len(runes) {
		character = len(runes)
	}

	start := character
	for start > 0 && isIdentChar(runes[start-1]) {
		start--
	}

	end := character
	for end < len(runes) && isIdentChar(runes[end]) {
		end++
	}

	if start >= end {
		return ""
	}
	return string(runes[start:end])
}

func isIdentChar(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '_', '.', '[', ']', '$':
		return true
	default:
		return false
	}
}

func findFactLocations(path string, schemaFiles []string) []lspLocation {
	locations := make([]lspLocation, 0)
	normalized := normalizeArrayPath(path)

	for _, file := range schemaFiles {
		index := indexFactPathsInFile(file)
		if locs, ok := index[path]; ok {
			locations = append(locations, locs...)
		}
		if normalized != path {
			if locs, ok := index[normalized]; ok {
				locations = append(locations, locs...)
			}
		}
	}

	return locations
}

func findVerbLocations(name string, verbFiles []string) []lspLocation {
	locations := make([]lspLocation, 0)
	for _, file := range verbFiles {
		index := indexVerbKeysInFile(file)
		if locs, ok := index[name]; ok {
			locations = append(locations, locs...)
		}
	}
	return locations
}

func indexFactPathsInFile(path string) map[string][]lspLocation {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string][]lspLocation{}
	}

	index := make(map[string][]lspLocation)
	matches := factPathPattern.FindAllSubmatchIndex(data, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		start := match[2]
		end := match[3]
		if start < 0 || end < 0 || end <= start || end > len(data) {
			continue
		}
		value := string(data[start:end])
		loc := locationFromOffsets(path, data, start, end)
		index[value] = append(index[value], loc)
	}
	return index
}

func indexVerbKeysInFile(path string) map[string][]lspLocation {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string][]lspLocation{}
	}

	index := make(map[string][]lspLocation)
	depth := 0
	inString := false
	escape := false
	keyStart := -1

	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
				if depth == 1 && keyStart >= 0 {
					j := i + 1
					for j < len(data) && isJSONWhitespace(data[j]) {
						j++
					}
					if j < len(data) && data[j] == ':' {
						key := string(data[keyStart:i])
						loc := locationFromOffsets(path, data, keyStart, i)
						index[key] = append(index[key], loc)
					}
				}
				keyStart = -1
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
			if depth == 1 {
				keyStart = i + 1
			}
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		}
	}

	return index
}

func isJSONWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func locationFromOffsets(path string, data []byte, start, end int) lspLocation {
	startLine, startCol := lineColForOffset(data, start)
	endLine, endCol := lineColForOffset(data, end)
	return lspLocation{
		URI: pathToURI(path),
		Range: lspRange{
			Start: lspPosition{Line: startLine, Character: startCol},
			End:   lspPosition{Line: endLine, Character: endCol},
		},
	}
}

func lineColForOffset(data []byte, offset int) (int, int) {
	if offset < 0 {
		return 0, 0
	}
	if offset > len(data) {
		offset = len(data)
	}
	line := 0
	col := 0
	for i := 0; i < offset; i++ {
		if data[i] == '\n' {
			line++
			col = 0
			continue
		}
		col++
	}
	return line, col
}

var arrayIndexPattern = regexp.MustCompile(`\[[0-9]+\]`)
var factPathPattern = regexp.MustCompile(`"path"\s*:\s*"([^"]+)"`)

func pathToURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	return "file://" + path
}

func normalizeArrayPath(path string) string {
	if path == "" {
		return path
	}
	return arrayIndexPattern.ReplaceAllString(path, "[]")
}

func (s *lspServer) respond(id json.RawMessage, result interface{}) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.write(resp)
}

func (s *lspServer) respondError(id json.RawMessage, code int, message string) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	}
	s.write(resp)
}

func (s *lspServer) notify(method string, params interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	s.write(msg)
}

func (s *lspServer) write(payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	_, _ = io.WriteString(s.writer, header)
	_, _ = s.writer.Write(data)
}

func readLSPMessage(reader *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				value := strings.TrimSpace(parts[1])
				if parsed, err := strconv.Atoi(value); err == nil {
					length = parsed
				}
			}
		}
	}

	if length <= 0 {
		return nil, fmt.Errorf("invalid content length")
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

func parsePathOption(value interface{}) []string {
	switch v := value.(type) {
	case string:
		return splitCommaList(v)
	case []interface{}:
		paths := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				paths = append(paths, str)
			}
		}
		return paths
	default:
		return nil
	}
}

func resolveSchemaFiles(paths []string) []string {
	resolved := make([]string, 0)
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.HasSuffix(entry.Name(), ".json") {
					resolved = append(resolved, filepath.Join(path, entry.Name()))
				}
			}
			continue
		}
		resolved = append(resolved, path)
	}

	return resolved
}

func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		trimmed := strings.TrimPrefix(uri, "file://")
		if strings.HasPrefix(trimmed, "/") {
			return trimmed
		}
		return "/" + trimmed
	}
	return uri
}

type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity,omitempty"`
	Source   string   `json:"source,omitempty"`
	Code     string   `json:"code,omitempty"`
	Message  string   `json:"message"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func issuesToLSPDiagnostics(issues []lint.Issue) []lspDiagnostic {
	out := make([]lspDiagnostic, 0, len(issues))
	for _, issue := range issues {
		line := issue.Pos.Line
		col := issue.Pos.Column
		if line <= 0 {
			line = 1
		}
		if col <= 0 {
			col = 1
		}
		sev := 2
		if strings.ToLower(issue.Severity) == "error" {
			sev = 1
		}

		start := lspPosition{Line: line - 1, Character: col - 1}
		end := lspPosition{Line: line - 1, Character: col}

		out = append(out, lspDiagnostic{
			Range:    lspRange{Start: start, End: end},
			Severity: sev,
			Source:   "effectusc",
			Code:     issue.Code,
			Message:  issue.Message,
		})
	}
	return out
}
