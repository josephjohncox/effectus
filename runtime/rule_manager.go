package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// RuleManager orchestrates the complete rule lifecycle
type RuleManager struct {
	// Storage backends
	storage       RuleStorageBackend
	gitIntegrator *GitIntegrator

	// Compilation and validation
	compiler  *RuleCompiler
	validator *RuleValidator

	// Deployment management
	deploymentController *DeploymentController

	// Configuration
	config *RuleManagerConfig

	// State
	activeRulesets map[string]*ActiveRuleset
	mu             sync.RWMutex
}

// RuleManagerConfig configures the rule manager
type RuleManagerConfig struct {
	// Git configuration
	GitRepository  string          `yaml:"git_repository"`
	GitBranch      string          `yaml:"git_branch"`
	GitCredentials *GitCredentials `yaml:"git_credentials"`
	RulesDirectory string          `yaml:"rules_directory"`

	// Environment configuration
	Environments []Environment `yaml:"environments"`
	DefaultEnv   string        `yaml:"default_environment"`

	// Compilation settings
	CompilerSettings *CompilerSettings `yaml:"compiler_settings"`

	// Deployment settings
	DeploymentSettings *DeploymentSettings `yaml:"deployment_settings"`

	// Validation settings
	ValidationSettings *ValidationSettings `yaml:"validation_settings"`

	// Monitoring and observability
	MetricsEnabled bool `yaml:"metrics_enabled"`
	AuditEnabled   bool `yaml:"audit_enabled"`

	// Hot reload configuration
	HotReloadEnabled bool          `yaml:"hot_reload_enabled"`
	PollInterval     time.Duration `yaml:"poll_interval"`
}

// Environment represents a deployment environment
type Environment struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"` // "development", "staging", "production"
	Config      map[string]string `yaml:"config"`
	Approvers   []string          `yaml:"approvers"`
	Constraints *EnvConstraints   `yaml:"constraints"`
}

// EnvConstraints defines environment-specific constraints
type EnvConstraints struct {
	RequireApproval   bool     `yaml:"require_approval"`
	AllowedUsers      []string `yaml:"allowed_users"`
	AllowedRoles      []string `yaml:"allowed_roles"`
	MaxRuleComplexity int      `yaml:"max_rule_complexity"`
	PerformanceBudget string   `yaml:"performance_budget"`
	SecurityPolicies  []string `yaml:"security_policies"`
}

// GitCredentials holds git authentication
type GitCredentials struct {
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	SSHKeyPath  string `yaml:"ssh_key_path"`
	AccessToken string `yaml:"access_token"`
}

// CompilerSettings configures rule compilation
type CompilerSettings struct {
	OptimizationLevel  string            `yaml:"optimization_level"`
	StrictValidation   bool              `yaml:"strict_validation"`
	PerformanceProfile bool              `yaml:"performance_profile"`
	GenerateMetrics    bool              `yaml:"generate_metrics"`
	TargetArchitecture string            `yaml:"target_architecture"`
	CustomFlags        map[string]string `yaml:"custom_flags"`
}

// DeploymentSettings configures deployment behavior
type DeploymentSettings struct {
	Strategy            string        `yaml:"strategy"`
	RolloutPercent      int           `yaml:"rollout_percent"`
	HealthCheckTimeout  time.Duration `yaml:"health_check_timeout"`
	RollbackOnFailure   bool          `yaml:"rollback_on_failure"`
	NotificationWebhook string        `yaml:"notification_webhook"`
}

// ValidationSettings configures rule validation
type ValidationSettings struct {
	SchemaValidation  bool            `yaml:"schema_validation"`
	PerformanceChecks bool            `yaml:"performance_checks"`
	SecurityScanning  bool            `yaml:"security_scanning"`
	CustomValidators  []string        `yaml:"custom_validators"`
	MaxExecutionTime  time.Duration   `yaml:"max_execution_time"`
	ResourceLimits    *ResourceLimits `yaml:"resource_limits"`
}

// ResourceLimits defines resource constraints
type ResourceLimits struct {
	MaxMemoryMB     int `yaml:"max_memory_mb"`
	MaxCPUPercent   int `yaml:"max_cpu_percent"`
	MaxNetworkCalls int `yaml:"max_network_calls"`
	MaxDiskUsageMB  int `yaml:"max_disk_usage_mb"`
}

// ActiveRuleset represents a currently active ruleset
type ActiveRuleset struct {
	Ruleset     *StoredRuleset
	Environment string
	LoadedAt    time.Time
	Status      ActiveStatus
	Health      *HealthStatus
	Metrics     *RulesetMetrics
}

// ActiveStatus represents the status of an active ruleset
type ActiveStatus string

const (
	ActiveStatusLoading  ActiveStatus = "loading"
	ActiveStatusActive   ActiveStatus = "active"
	ActiveStatusDraining ActiveStatus = "draining"
	ActiveStatusError    ActiveStatus = "error"
)

// HealthStatus tracks ruleset health
type HealthStatus struct {
	IsHealthy      bool          `json:"is_healthy"`
	LastCheck      time.Time     `json:"last_check"`
	SuccessRate    float64       `json:"success_rate"`
	AverageLatency time.Duration `json:"average_latency"`
	ErrorRate      float64       `json:"error_rate"`
	LastError      string        `json:"last_error,omitempty"`
}

// RulesetMetrics tracks performance metrics
type RulesetMetrics struct {
	ExecutionCount int64         `json:"execution_count"`
	SuccessCount   int64         `json:"success_count"`
	ErrorCount     int64         `json:"error_count"`
	TotalLatency   time.Duration `json:"total_latency"`
	AverageLatency time.Duration `json:"average_latency"`
	MinLatency     time.Duration `json:"min_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	LastExecuted   time.Time     `json:"last_executed"`
	MemoryUsage    int64         `json:"memory_usage"`
	CPUUsage       float64       `json:"cpu_usage"`
}

// GitIntegrator handles git operations
type GitIntegrator struct {
	repository *git.Repository
	config     *GitIntegratorConfig
}

// GitIntegratorConfig configures git integration
type GitIntegratorConfig struct {
	RepoURL       string          `yaml:"repo_url"`
	Branch        string          `yaml:"branch"`
	Credentials   *GitCredentials `yaml:"credentials"`
	LocalPath     string          `yaml:"local_path"`
	RulesPath     string          `yaml:"rules_path"`
	AutoPull      bool            `yaml:"auto_pull"`
	PullInterval  time.Duration   `yaml:"pull_interval"`
	WebhookSecret string          `yaml:"webhook_secret"`
}

// NewRuleManager creates a new rule manager
func NewRuleManager(storage RuleStorageBackend, config *RuleManagerConfig) (*RuleManager, error) {
	manager := &RuleManager{
		storage:        storage,
		config:         config,
		activeRulesets: make(map[string]*ActiveRuleset),
	}

	// Initialize git integrator
	if config.GitRepository != "" {
		gitConfig := &GitIntegratorConfig{
			RepoURL:      config.GitRepository,
			Branch:       config.GitBranch,
			Credentials:  config.GitCredentials,
			LocalPath:    filepath.Join(os.TempDir(), "effectus-rules"),
			RulesPath:    config.RulesDirectory,
			AutoPull:     true,
			PullInterval: config.PollInterval,
		}

		gitIntegrator, err := NewGitIntegrator(gitConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize git integrator: %w", err)
		}
		manager.gitIntegrator = gitIntegrator
	}

	// Initialize compiler
	manager.compiler = NewRuleCompiler(config.CompilerSettings)

	// Initialize validator
	manager.validator = NewRuleValidator(config.ValidationSettings)

	// Initialize deployment controller
	manager.deploymentController = NewDeploymentController(config.DeploymentSettings, storage)

	return manager, nil
}

// Start starts the rule manager
func (rm *RuleManager) Start(ctx context.Context) error {
	// Start git polling if enabled
	if rm.gitIntegrator != nil && rm.config.HotReloadEnabled {
		go rm.gitPollLoop(ctx)
	}

	// Load active rulesets from storage
	if err := rm.loadActiveRulesets(ctx); err != nil {
		return fmt.Errorf("failed to load active rulesets: %w", err)
	}

	return nil
}

// DeployFromGit pulls latest rules from git and deploys them
func (rm *RuleManager) DeployFromGit(ctx context.Context, environment string, options *DeploymentOptions) (*DeploymentResult, error) {
	// Pull latest changes
	if rm.gitIntegrator == nil {
		return nil, fmt.Errorf("git integration not configured")
	}

	commitInfo, err := rm.gitIntegrator.PullLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to pull from git: %w", err)
	}

	// Discover and compile rules
	rules, err := rm.discoverRules(ctx, rm.gitIntegrator.GetLocalPath())
	if err != nil {
		return nil, fmt.Errorf("failed to discover rules: %w", err)
	}

	// Compile rules
	compiledRulesets, err := rm.compileRules(ctx, rules)
	if err != nil {
		return nil, fmt.Errorf("failed to compile rules: %w", err)
	}

	// Validate compiled rules
	for _, ruleset := range compiledRulesets {
		if err := rm.validator.ValidateRuleset(ctx, ruleset); err != nil {
			return nil, fmt.Errorf("validation failed for ruleset %s: %w", ruleset.Name, err)
		}
	}

	// Store compiled rulesets
	var deploymentResults []*DeploymentResult
	for _, ruleset := range compiledRulesets {
		storedRuleset := &StoredRuleset{
			Ruleset:         ruleset,
			Name:            ruleset.Name,
			Version:         commitInfo.Hash,
			Environment:     environment,
			CreatedAt:       time.Now(),
			CreatedBy:       commitInfo.Author,
			GitCommit:       commitInfo.Hash,
			GitBranch:       commitInfo.Branch,
			GitAuthor:       commitInfo.Author,
			CompiledAt:      time.Now(),
			CompilerVersion: rm.compiler.GetVersion(),
			Status:          RulesetStatusReady,
			Deployments:     make(map[string]*Deployment),
		}

		// Store ruleset
		if err := rm.storage.StoreRuleset(ctx, storedRuleset); err != nil {
			return nil, fmt.Errorf("failed to store ruleset %s: %w", ruleset.Name, err)
		}

		// Deploy ruleset
		deployResult, err := rm.deploymentController.Deploy(ctx, storedRuleset, environment, options)
		if err != nil {
			return nil, fmt.Errorf("failed to deploy ruleset %s: %w", ruleset.Name, err)
		}

		deploymentResults = append(deploymentResults, deployResult)
	}

	// Return aggregated result
	return &DeploymentResult{
		Success:     true,
		Environment: environment,
		Version:     commitInfo.Hash,
		DeployedAt:  time.Now(),
		Rulesets:    len(compiledRulesets),
		Details:     deploymentResults,
		CommitInfo:  commitInfo,
	}, nil
}

// DeploymentOptions configures deployment behavior
type DeploymentOptions struct {
	Strategy       string            `json:"strategy"`
	DryRun         bool              `json:"dry_run"`
	Force          bool              `json:"force"`
	RolloutPercent int               `json:"rollout_percent"`
	HealthCheck    bool              `json:"health_check"`
	Timeout        time.Duration     `json:"timeout"`
	Metadata       map[string]string `json:"metadata"`
}

// DeploymentResult contains deployment outcome
type DeploymentResult struct {
	Success     bool                `json:"success"`
	Environment string              `json:"environment"`
	Version     string              `json:"version"`
	DeployedAt  time.Time           `json:"deployed_at"`
	Rulesets    int                 `json:"rulesets"`
	Details     []*DeploymentResult `json:"details,omitempty"`
	CommitInfo  *CommitInfo         `json:"commit_info"`
	Errors      []string            `json:"errors,omitempty"`
	Warnings    []string            `json:"warnings,omitempty"`
}

// CommitInfo contains git commit information
type CommitInfo struct {
	Hash      string    `json:"hash"`
	Author    string    `json:"author"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Branch    string    `json:"branch"`
	Tag       string    `json:"tag,omitempty"`
}

// NewGitIntegrator creates a new git integrator
func NewGitIntegrator(config *GitIntegratorConfig) (*GitIntegrator, error) {
	// Clone repository if it doesn't exist locally
	if _, err := os.Stat(config.LocalPath); os.IsNotExist(err) {
		if err := os.MkdirAll(config.LocalPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create local path: %w", err)
		}

		_, err := git.PlainClone(config.LocalPath, false, &git.CloneOptions{
			URL:           config.RepoURL,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", config.Branch)),
			SingleBranch:  true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to clone repository: %w", err)
		}
	}

	// Open existing repository
	repo, err := git.PlainOpen(config.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	return &GitIntegrator{
		repository: repo,
		config:     config,
	}, nil
}

// PullLatest pulls the latest changes from the remote repository
func (gi *GitIntegrator) PullLatest(ctx context.Context) (*CommitInfo, error) {
	workTree, err := gi.repository.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Pull latest changes
	err = workTree.Pull(&git.PullOptions{
		ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", gi.config.Branch)),
		SingleBranch:  true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return nil, fmt.Errorf("failed to pull: %w", err)
	}

	// Get latest commit info
	ref, err := gi.repository.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commit, err := gi.repository.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	return &CommitInfo{
		Hash:      ref.Hash().String(),
		Author:    commit.Author.Email,
		Message:   commit.Message,
		Timestamp: commit.Author.When,
		Branch:    gi.config.Branch,
	}, nil
}

// GetLocalPath returns the local repository path
func (gi *GitIntegrator) GetLocalPath() string {
	return filepath.Join(gi.config.LocalPath, gi.config.RulesPath)
}

// discoverRules finds all rule files in the given directory
func (rm *RuleManager) discoverRules(ctx context.Context, rulesDir string) ([]RuleFile, error) {
	var ruleFiles []RuleFile

	err := filepath.Walk(rulesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && (strings.HasSuffix(path, ".eff") || strings.HasSuffix(path, ".effx")) {
			ruleFile := RuleFile{
				Path:         path,
				Name:         strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
				Type:         filepath.Ext(path),
				ModifiedTime: info.ModTime(),
			}
			ruleFiles = append(ruleFiles, ruleFile)
		}

		return nil
	})

	return ruleFiles, err
}

// RuleFile represents a rule file
type RuleFile struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	ModifiedTime time.Time `json:"modified_time"`
	Content      string    `json:"content,omitempty"`
}

// compileRules compiles a set of rule files
func (rm *RuleManager) compileRules(ctx context.Context, ruleFiles []RuleFile) ([]*CompiledRuleset, error) {
	var compiledRulesets []*CompiledRuleset

	// Group rule files by ruleset (based on directory structure or naming)
	rulesetGroups := rm.groupRuleFiles(ruleFiles)

	for rulesetName, files := range rulesetGroups {
		compiledRuleset, err := rm.compiler.CompileRuleset(ctx, rulesetName, files)
		if err != nil {
			return nil, fmt.Errorf("failed to compile ruleset %s: %w", rulesetName, err)
		}
		compiledRulesets = append(compiledRulesets, compiledRuleset)
	}

	return compiledRulesets, nil
}

// groupRuleFiles groups rule files by ruleset
func (rm *RuleManager) groupRuleFiles(ruleFiles []RuleFile) map[string][]RuleFile {
	groups := make(map[string][]RuleFile)

	for _, file := range ruleFiles {
		// Extract ruleset name from path (e.g., "rules/user_management/welcome.eff" -> "user_management")
		pathParts := strings.Split(file.Path, string(filepath.Separator))
		var rulesetName string

		if len(pathParts) > 2 {
			rulesetName = pathParts[len(pathParts)-2]
		} else {
			rulesetName = "default"
		}

		groups[rulesetName] = append(groups[rulesetName], file)
	}

	return groups
}

// gitPollLoop continuously polls git for changes
func (rm *RuleManager) gitPollLoop(ctx context.Context) {
	ticker := time.NewTicker(rm.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := rm.checkForGitUpdates(ctx); err != nil {
				// Log error but continue polling
				fmt.Printf("Git poll error: %v\n", err)
			}
		}
	}
}

// checkForGitUpdates checks for updates and triggers redeployment if needed
func (rm *RuleManager) checkForGitUpdates(ctx context.Context) error {
	if rm.gitIntegrator == nil {
		return nil
	}

	commitInfo, err := rm.gitIntegrator.PullLatest(ctx)
	if err != nil {
		return fmt.Errorf("failed to check git updates: %w", err)
	}

	// Check if this is a new commit
	lastCommit := rm.getLastDeployedCommit()
	if lastCommit == commitInfo.Hash {
		return nil // No changes
	}

	// Auto-deploy to configured environments
	for _, env := range rm.config.Environments {
		if env.Type == "development" { // Only auto-deploy to dev
			options := &DeploymentOptions{
				Strategy:    "rolling",
				DryRun:      false,
				Force:       false,
				HealthCheck: true,
				Timeout:     5 * time.Minute,
			}

			result, err := rm.DeployFromGit(ctx, env.Name, options)
			if err != nil {
				return fmt.Errorf("auto-deployment failed for %s: %w", env.Name, err)
			}

			// Log successful deployment
			fmt.Printf("Auto-deployed to %s: %s\n", env.Name, result.Version)
		}
	}

	return nil
}

// getLastDeployedCommit returns the last deployed git commit hash
func (rm *RuleManager) getLastDeployedCommit() string {
	// Implementation would query storage for last deployed commit
	return ""
}

// loadActiveRulesets loads currently active rulesets from storage
func (rm *RuleManager) loadActiveRulesets(ctx context.Context) error {
	filters := &RulesetFilters{
		Status: []RulesetStatus{RulesetStatusDeployed},
	}

	rulesets, err := rm.storage.ListRulesets(ctx, filters)
	if err != nil {
		return fmt.Errorf("failed to list active rulesets: %w", err)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	for _, metadata := range rulesets {
		ruleset, err := rm.storage.GetRuleset(ctx, metadata.Name, metadata.Version)
		if err != nil {
			continue // Skip failed loads
		}

		activeRuleset := &ActiveRuleset{
			Ruleset:     ruleset,
			Environment: metadata.Environment,
			LoadedAt:    time.Now(),
			Status:      ActiveStatusActive,
			Health:      &HealthStatus{IsHealthy: true, LastCheck: time.Now()},
			Metrics:     &RulesetMetrics{},
		}

		key := fmt.Sprintf("%s:%s", metadata.Name, metadata.Environment)
		rm.activeRulesets[key] = activeRuleset
	}

	return nil
}
