package runtime

import "net/http"

// SetMemorySyncDependenciesForTest supplies only the foreground sync dependencies.
func SetMemorySyncDependenciesForTest(target *Memory, transport http.RoundTripper, credential func(string) (string, error)) {
	target.transport = transport
	target.credential = credential
}
