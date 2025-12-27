package unified

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// ResolverOptions configures how bundle manifests are resolved.
type ResolverOptions struct {
	CacheDir        string
	DefaultRegistry string
	Registries      []RegistryConfig
	EngineVersion   string
	VerifyChecksum  bool
	AllowFile       bool
	AllowOCI        bool
}

// ResolvedBundle describes a resolved bundle dependency.
type ResolvedBundle struct {
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	Source     string  `json:"source"`
	Location   string  `json:"location"`
	RootDir    string  `json:"root_dir"`
	BundlePath string  `json:"bundle_path"`
	Checksum   string  `json:"checksum"`
	Bundle     *Bundle `json:"-"`
}

// ResolveManifestFile resolves bundles from a manifest on disk.
func ResolveManifestFile(path string, opts ResolverOptions) ([]ResolvedBundle, error) {
	manifest, err := LoadExtensionManifest(path)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(path)
	return ResolveManifest(manifest, baseDir, opts)
}

// ResolveManifest resolves bundles from a manifest.
func ResolveManifest(manifest *ExtensionManifest, baseDir string, opts ResolverOptions) ([]ResolvedBundle, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is nil")
	}

	if opts.AllowFile == false && opts.AllowOCI == false {
		// Allow both by default if neither is explicitly set.
		opts.AllowFile = true
		opts.AllowOCI = true
	}

	registries, defaultRegistry := mergeRegistries(manifest.Registries, opts)

	if manifest.Effectus != "" && opts.EngineVersion != "" {
		ok, err := satisfiesConstraint(opts.EngineVersion, manifest.Effectus)
		if err != nil {
			return nil, fmt.Errorf("invalid effectus constraint: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("effectus version %s does not satisfy manifest constraint %s", opts.EngineVersion, manifest.Effectus)
		}
	}

	resolved := make([]ResolvedBundle, 0, len(manifest.Bundles))
	var errs []string

	for _, req := range manifest.Bundles {
		bundle, err := resolveBundleRequirement(req, baseDir, registries, defaultRegistry, opts)
		if err != nil {
			if req.Optional {
				continue
			}
			errs = append(errs, fmt.Sprintf("%s: %v", req.Name, err))
			continue
		}
		resolved = append(resolved, bundle)
	}

	if len(errs) > 0 {
		return resolved, errors.New(strings.Join(errs, "; "))
	}

	if err := checkBundleCompatibility(manifest, resolved, opts); err != nil {
		return resolved, err
	}

	return resolved, nil
}

func mergeRegistries(manifestRegs []RegistryConfig, opts ResolverOptions) (map[string]RegistryConfig, string) {
	registries := make(map[string]RegistryConfig)
	defaultName := strings.TrimSpace(opts.DefaultRegistry)

	for _, reg := range manifestRegs {
		if reg.Name == "" {
			continue
		}
		registries[reg.Name] = reg
		if reg.Default && defaultName == "" {
			defaultName = reg.Name
		}
	}

	for _, reg := range opts.Registries {
		name := strings.TrimSpace(reg.Name)
		if name == "" {
			if defaultName == "" {
				name = "default"
				defaultName = name
			} else {
				name = defaultName
			}
			reg.Name = name
			reg.Default = true
		}
		registries[name] = reg
		if reg.Default {
			defaultName = name
		}
	}

	if defaultName == "" {
		for name := range registries {
			defaultName = name
			break
		}
	}

	return registries, defaultName
}

func resolveBundleRequirement(req BundleRequirement, baseDir string, registries map[string]RegistryConfig, defaultRegistry string, opts ResolverOptions) (ResolvedBundle, error) {
	if req.Name == "" {
		return ResolvedBundle{}, fmt.Errorf("bundle name is required")
	}

	if hasMultipleSources(req) {
		return ResolvedBundle{}, fmt.Errorf("bundle %s must specify only one source", req.Name)
	}

	if req.File != "" {
		if !opts.AllowFile {
			return ResolvedBundle{}, fmt.Errorf("file source not allowed")
		}
		return resolveFileBundle(req, baseDir, opts)
	}

	if req.OCI != "" {
		if !opts.AllowOCI {
			return ResolvedBundle{}, fmt.Errorf("oci source not allowed")
		}
		return resolveOCIBundle(req, opts)
	}

	regName := req.Registry
	if regName == "" {
		regName = defaultRegistry
	}
	if regName == "" {
		return ResolvedBundle{}, fmt.Errorf("no registry configured")
	}

	reg, ok := registries[regName]
	if !ok {
		return ResolvedBundle{}, fmt.Errorf("registry %s not found", regName)
	}

	regType := registryType(reg)
	switch regType {
	case "file":
		return resolveFileRegistryBundle(req, reg, opts)
	default:
		return resolveOCIRegistryBundle(req, reg, opts)
	}
}

func hasMultipleSources(req BundleRequirement) bool {
	count := 0
	if req.File != "" {
		count++
	}
	if req.OCI != "" {
		count++
	}
	return count > 1
}

func resolveFileBundle(req BundleRequirement, baseDir string, opts ResolverOptions) (ResolvedBundle, error) {
	path := req.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	bundlePath, rootDir, err := normalizeBundlePath(path)
	if err != nil {
		return ResolvedBundle{}, err
	}

	bundleData, err := os.ReadFile(bundlePath)
	if err != nil {
		return ResolvedBundle{}, fmt.Errorf("reading bundle: %w", err)
	}

	bundle, err := LoadBundle(bundlePath)
	if err != nil {
		return ResolvedBundle{}, err
	}

	if err := verifyBundleConstraints(req, bundle); err != nil {
		return ResolvedBundle{}, err
	}

	checksum := checksumBytes(bundleData)
	if opts.VerifyChecksum && req.Checksum != "" {
		if !checksumsMatch(req.Checksum, checksum) {
			return ResolvedBundle{}, fmt.Errorf("checksum mismatch: expected %s, got %s", normalizeChecksum(req.Checksum), checksum)
		}
	}

	return ResolvedBundle{
		Name:       bundle.Name,
		Version:    bundle.Version,
		Source:     "file",
		Location:   bundlePath,
		RootDir:    rootDir,
		BundlePath: bundlePath,
		Checksum:   checksum,
		Bundle:     bundle,
	}, nil
}

func resolveFileRegistryBundle(req BundleRequirement, reg RegistryConfig, opts ResolverOptions) (ResolvedBundle, error) {
	base := strings.TrimPrefix(reg.Base, "file://")
	if base == "" {
		return ResolvedBundle{}, fmt.Errorf("file registry base is empty")
	}

	version := req.Version
	if version == "" {
		return ResolvedBundle{}, fmt.Errorf("version constraint required")
	}

	if !isExactVersionConstraint(version) {
		available, err := listFileRegistryVersions(base, req.Name)
		if err != nil {
			return ResolvedBundle{}, err
		}
		version, err = selectHighestVersion(available, version)
		if err != nil {
			return ResolvedBundle{}, err
		}
	}

	bundlePath := filepath.Join(base, req.Name, version, "bundle.json")
	return resolveFileBundle(BundleRequirement{
		Name:          req.Name,
		Version:       version,
		File:          bundlePath,
		Checksum:      req.Checksum,
		Optional:      req.Optional,
		Compatibility: req.Compatibility,
	}, "", opts)
}

func resolveOCIBundle(req BundleRequirement, opts ResolverOptions) (ResolvedBundle, error) {
	ref := strings.TrimSpace(req.OCI)
	if ref == "" {
		return ResolvedBundle{}, fmt.Errorf("oci reference is empty")
	}

	cacheDir := bundleCacheDir(opts.CacheDir)
	bundleDir := filepath.Join(cacheDir, sanitizeRefName(req.Name), sanitizeRefName(req.Version))
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return ResolvedBundle{}, fmt.Errorf("creating bundle cache: %w", err)
	}

	puller := NewOCIBundlePuller(bundleDir)
	bundle, bundleData, err := puller.PullWithData(ref)
	if err != nil {
		return ResolvedBundle{}, err
	}

	if err := verifyBundleConstraints(req, bundle); err != nil {
		return ResolvedBundle{}, err
	}

	checksum := checksumBytes(bundleData)
	if opts.VerifyChecksum && req.Checksum != "" {
		if !checksumsMatch(req.Checksum, checksum) {
			return ResolvedBundle{}, fmt.Errorf("checksum mismatch: expected %s, got %s", normalizeChecksum(req.Checksum), checksum)
		}
	}

	bundlePath := filepath.Join(bundleDir, "bundle.json")
	return ResolvedBundle{
		Name:       bundle.Name,
		Version:    bundle.Version,
		Source:     "oci",
		Location:   ref,
		RootDir:    bundleDir,
		BundlePath: bundlePath,
		Checksum:   checksum,
		Bundle:     bundle,
	}, nil
}

func resolveOCIRegistryBundle(req BundleRequirement, reg RegistryConfig, opts ResolverOptions) (ResolvedBundle, error) {
	base := strings.TrimSuffix(strings.TrimSpace(reg.Base), "/")
	if base == "" {
		return ResolvedBundle{}, fmt.Errorf("registry base is empty")
	}

	version := req.Version
	if version == "" {
		return ResolvedBundle{}, fmt.Errorf("version constraint required")
	}

	if !isExactVersionConstraint(version) {
		available, err := listOCIVersions(base, req.Name)
		if err != nil {
			return ResolvedBundle{}, err
		}
		version, err = selectHighestVersion(available, version)
		if err != nil {
			return ResolvedBundle{}, err
		}
	}

	ref := fmt.Sprintf("%s/%s:%s", base, req.Name, version)
	return resolveOCIBundle(BundleRequirement{
		Name:          req.Name,
		Version:       version,
		OCI:           ref,
		Checksum:      req.Checksum,
		Optional:      req.Optional,
		Compatibility: req.Compatibility,
	}, opts)
}

func listOCIVersions(base, bundleName string) ([]string, error) {
	repo, err := name.NewRepository(fmt.Sprintf("%s/%s", base, bundleName))
	if err != nil {
		return nil, fmt.Errorf("parsing registry repo: %w", err)
	}

	tags, err := remote.List(repo, remote.WithContext(context.Background()))
	if err != nil {
		return nil, fmt.Errorf("listing registry tags: %w", err)
	}
	return tags, nil
}

func listFileRegistryVersions(base, bundleName string) ([]string, error) {
	dir := filepath.Join(base, bundleName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading registry directory %s: %w", dir, err)
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		versions = append(versions, entry.Name())
	}
	return versions, nil
}

func verifyBundleConstraints(req BundleRequirement, bundle *Bundle) error {
	if bundle == nil {
		return fmt.Errorf("bundle metadata missing")
	}
	if req.Name != "" && bundle.Name != req.Name {
		return fmt.Errorf("bundle name mismatch: expected %s, got %s", req.Name, bundle.Name)
	}
	if req.Version != "" {
		ok, err := satisfiesConstraint(bundle.Version, req.Version)
		if err != nil {
			return fmt.Errorf("invalid version constraint: %w", err)
		}
		if !ok {
			return fmt.Errorf("bundle version %s does not satisfy constraint %s", bundle.Version, req.Version)
		}
	}
	return nil
}

func normalizeBundlePath(path string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("stat bundle path: %w", err)
	}
	if info.IsDir() {
		bundlePath := filepath.Join(path, "bundle.json")
		return bundlePath, path, nil
	}
	return path, filepath.Dir(path), nil
}

func bundleCacheDir(cacheDir string) string {
	if strings.TrimSpace(cacheDir) != "" {
		return cacheDir
	}
	if env := strings.TrimSpace(os.Getenv("EFFECTUS_BUNDLE_CACHE")); env != "" {
		return env
	}
	return "./bundles"
}

func sanitizeRefName(value string) string {
	if value == "" {
		return "default"
	}
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, ":", "-")
	return value
}

func registryType(reg RegistryConfig) string {
	if reg.Type != "" {
		return strings.ToLower(reg.Type)
	}
	base := strings.TrimSpace(reg.Base)
	if strings.HasPrefix(base, "file://") || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "/") {
		return "file"
	}
	return "oci"
}

func checksumBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeChecksum(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	trimmed = strings.TrimPrefix(trimmed, "sha256:")
	return "sha256:" + trimmed
}

func checksumsMatch(expected, actual string) bool {
	return normalizeChecksum(expected) == normalizeChecksum(actual)
}

func isExactVersionConstraint(version string) bool {
	v := strings.TrimSpace(version)
	if v == "" {
		return false
	}
	if strings.ContainsAny(v, "<>=^~*") {
		return false
	}
	if strings.ContainsAny(v, "xX") {
		return false
	}
	return true
}

func checkBundleCompatibility(manifest *ExtensionManifest, resolved []ResolvedBundle, opts ResolverOptions) error {
	if manifest == nil {
		return nil
	}

	versions := make(map[string]string)
	for _, bundle := range resolved {
		versions[bundle.Name] = bundle.Version
	}

	for _, req := range manifest.Bundles {
		if req.Compatibility == nil {
			continue
		}
		if req.Compatibility.Effectus != "" && opts.EngineVersion != "" {
			ok, err := satisfiesConstraint(opts.EngineVersion, req.Compatibility.Effectus)
			if err != nil {
				return fmt.Errorf("invalid effectus constraint for %s: %w", req.Name, err)
			}
			if !ok {
				return fmt.Errorf("effectus version %s does not satisfy %s requirement %s", opts.EngineVersion, req.Name, req.Compatibility.Effectus)
			}
		}
		for depName, constraint := range req.Compatibility.Bundles {
			depVersion, ok := versions[depName]
			if !ok {
				return fmt.Errorf("bundle %s requires %s but it is not resolved", req.Name, depName)
			}
			okMatch, err := satisfiesConstraint(depVersion, constraint)
			if err != nil {
				return fmt.Errorf("invalid compatibility constraint for %s on %s: %w", req.Name, depName, err)
			}
			if !okMatch {
				return fmt.Errorf("bundle %s requires %s %s, got %s", req.Name, depName, constraint, depVersion)
			}
		}
	}

	return nil
}
