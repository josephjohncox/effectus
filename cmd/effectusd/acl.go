package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type aclConfig struct {
	DefaultRole string    `yaml:"default_role" json:"default_role"`
	Rules       []aclRule `yaml:"rules" json:"rules"`
}

type aclRule struct {
	Path    string   `yaml:"path" json:"path"`
	Methods []string `yaml:"methods" json:"methods"`
	Role    string   `yaml:"role" json:"role"`
}

type aclMatcher struct {
	defaultRole apiRole
	hasDefault  bool
	rules       []aclRuleMatcher
}

type aclRuleMatcher struct {
	path    string
	methods map[string]struct{}
	role    apiRole
	prefix  bool
}

func loadACL(path string) (*aclMatcher, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, errors.New("acl file is empty")
	}

	var config aclConfig
	if err := yaml.Unmarshal(payload, &config); err != nil {
		return nil, err
	}
	return compileACL(config)
}

func compileACL(config aclConfig) (*aclMatcher, error) {
	matcher := &aclMatcher{}
	if config.DefaultRole != "" {
		role, err := parseRole(config.DefaultRole)
		if err != nil {
			return nil, fmt.Errorf("default_role: %w", err)
		}
		matcher.defaultRole = role
		matcher.hasDefault = true
	}

	for _, rule := range config.Rules {
		if strings.TrimSpace(rule.Path) == "" {
			continue
		}
		role, err := parseRole(rule.Role)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.Path, err)
		}
		methods := make(map[string]struct{})
		for _, method := range rule.Methods {
			method = strings.ToUpper(strings.TrimSpace(method))
			if method == "" {
				continue
			}
			methods[method] = struct{}{}
		}
		path := strings.TrimSpace(rule.Path)
		prefix := strings.HasSuffix(path, "*")
		if prefix {
			path = strings.TrimSuffix(path, "*")
		}
		matcher.rules = append(matcher.rules, aclRuleMatcher{
			path:    path,
			methods: methods,
			role:    role,
			prefix:  prefix,
		})
	}

	return matcher, nil
}

func parseRole(input string) (apiRole, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "read", "r":
		return roleRead, nil
	case "write", "w":
		return roleWrite, nil
	default:
		return roleRead, fmt.Errorf("unknown role %q", input)
	}
}

func (a *aclMatcher) match(r *http.Request) (apiRole, bool) {
	if a == nil || r == nil {
		return roleRead, false
	}
	path := r.URL.Path
	method := strings.ToUpper(r.Method)
	for _, rule := range a.rules {
		if rule.prefix {
			if !strings.HasPrefix(path, rule.path) {
				continue
			}
		} else if path != rule.path {
			continue
		}
		if len(rule.methods) > 0 {
			if _, ok := rule.methods[method]; !ok {
				continue
			}
		}
		return rule.role, true
	}
	return roleRead, false
}

func (a *aclMatcher) requiredRole(r *http.Request, fallback apiRole) apiRole {
	if a == nil {
		return fallback
	}
	if role, ok := a.match(r); ok {
		return role
	}
	if a.hasDefault {
		return a.defaultRole
	}
	return fallback
}
