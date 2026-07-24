package sensitivepaths

import (
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// relativePatterns is the canonical repository-relative deny policy shared by
// evidence collection and provider file permissions. Keep entries relative so
// callers can apply the same policy at any workspace root.
var relativePatterns = []string{
	".env", ".env.*",
	"*.key", "*.pem", "*.p12", "*.pfx", "*.jks", "*.keystore",
	".npmrc", ".pypirc", ".netrc", ".git-credentials", ".authinfo", ".authinfo.gpg",
	"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "id_xmss",
	"credentials.json", "credentials.yml", "credentials.yaml", "credentials.toml",
	"service-account*.json", "secrets.*",
	".aws/credentials", ".credentials", ".credentials/**",
	".git", ".git/**",
	".ssh", ".ssh/**", "secrets", "secrets/**",
	".kube/config", ".docker/config.json", ".config/gh/hosts.yml",
	".config/gcloud/application_default_credentials.json",
	".azure/accessTokens.json", ".azure/msal_token_cache.json",
	".terraform.d/credentials.tfrc.json", "*.tfstate", "*.tfstate.*",
}

// OpenCodeDenyPatterns returns root and nested patterns for OpenCode's simple
// wildcard matcher. The returned slice is detached from the package policy.
func OpenCodeDenyPatterns() []string {
	patterns := make([]string, 0, len(relativePatterns)*2)
	for _, pattern := range relativePatterns {
		patterns = append(patterns, pattern, "**/"+pattern)
	}
	return patterns
}

// GitExcludePathspecs returns case-insensitive exclusions so repository casing
// cannot bypass the evidence boundary. Git pathspecs apply to tracked, staged,
// and untracked status collection.
func GitExcludePathspecs() []string {
	patterns := OpenCodeDenyPatterns()
	pathspecs := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pathspecs = append(pathspecs, ":(icase,exclude)"+pattern)
	}
	return pathspecs
}

// IsSensitive applies the canonical policy case-insensitively to one relative
// path. It is used as defense in depth when a consumer builds an allowlist.
func IsSensitive(relative string) bool {
	if filepath.IsAbs(relative) {
		return true
	}
	cleaned := filepath.ToSlash(filepath.Clean(relative))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return true
	}
	parts := strings.Split(strings.ToLower(cleaned), "/")
	for start := range parts {
		candidate := strings.Join(parts[start:], "/")
		for _, original := range relativePatterns {
			pattern := strings.ToLower(original)
			if strings.HasSuffix(pattern, "/**") {
				directory := strings.TrimSuffix(pattern, "/**")
				if candidate == directory || strings.HasPrefix(candidate, directory+"/") {
					return true
				}
				continue
			}
			if matched, _ := path.Match(pattern, candidate); matched {
				return true
			}
		}
	}
	return false
}

// ContainsSensitiveReference detects denied paths embedded in tool output.
// Absolute paths inside root are normalized back to repository-relative form.
func ContainsSensitiveReference(value, root string) bool {
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("\"'`[](){}<>,;:=", r)
	}) {
		candidate := strings.TrimSpace(field)
		if candidate == "" {
			continue
		}
		if filepath.IsAbs(candidate) {
			volume := filepath.VolumeName(candidate)
			if strings.Trim(strings.TrimPrefix(candidate, volume), `/\`) == "" {
				continue
			}
			relative, err := filepath.Rel(root, candidate)
			if err != nil || !filepath.IsLocal(relative) {
				return true
			}
			candidate = relative
		}
		if IsSensitive(candidate) {
			return true
		}
	}
	return false
}
