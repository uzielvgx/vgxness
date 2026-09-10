package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRetiredSDDRejectsMutationBeforeRuntimeAccess(t *testing.T) {
	for _, args := range [][]string{{"sdd", "create"}, {"sdd", "transition"}, {"sdd-archive", "create"}, {"sdd-archive", "accept-revision"}, {"sdd-archive", "list"}, {"sdd-archive", "get"}, {"sdd-archive", "list-revisions"}, {"sdd-archive", "get-revision"}} {
		var out, diagnostic bytes.Buffer
		code := RunProductRuntime(context.Background(), args, strings.NewReader("{}"), &out, &diagnostic, nil, nil, nil, nil, nil, nil)
		if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("%v: code=%d output=%q diagnostic=%q", args, code, out.String(), diagnostic.String())
		}
	}
}
