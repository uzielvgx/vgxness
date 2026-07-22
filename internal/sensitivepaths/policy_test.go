package sensitivepaths

import "testing"

func TestPolicyCoversCommonCredentialLocations(t *testing.T) {
	openCode := stringSet(OpenCodeDenyPatterns())
	git := stringSet(GitExcludePathspecs())
	for _, pattern := range []string{
		".npmrc", "**/.pypirc", "**/.netrc", "**/id_rsa", "**/id_ed25519",
		"**/credentials.json", "**/.docker/config.json", "**/.config/gh/hosts.yml",
		"**/*.p12", "**/*.tfstate", ".git", "**/.git/**",
	} {
		if !openCode[pattern] {
			t.Fatalf("OpenCode policy does not deny %q", pattern)
		}
		if !git[":(icase,exclude)"+pattern] {
			t.Fatalf("Git policy does not exclude %q", pattern)
		}
	}
}

func TestPolicyReturnsDetachedSlices(t *testing.T) {
	first := OpenCodeDenyPatterns()
	first[0] = "changed"
	if OpenCodeDenyPatterns()[0] == "changed" {
		t.Fatal("OpenCode policy leaked mutable package state")
	}
	git := GitExcludePathspecs()
	git[0] = "changed"
	if GitExcludePathspecs()[0] == "changed" {
		t.Fatal("Git policy leaked mutable package state")
	}
}

func TestIsSensitiveIsNestedAndCaseInsensitive(t *testing.T) {
	for _, candidate := range []string{".NPMRC", "nested/.PyPiRc", "keys/ID_ED25519", "config/Credentials.JSON", "nested/.SSH/config", "state/PROD.TFSTATE", ".git/config", "nested/.GIT/objects/pack"} {
		if !IsSensitive(candidate) {
			t.Fatalf("policy did not recognize %q", candidate)
		}
	}
	for _, candidate := range []string{"README.md", "internal/credentials/parser.go", "docs/secrets-guide.md"} {
		if IsSensitive(candidate) {
			t.Fatalf("policy rejected safe path %q", candidate)
		}
	}
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
