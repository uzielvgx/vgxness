package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRetiredSDDRejectsMutationBeforeRuntimeAccess(t *testing.T) {
	for _, args := range [][]string{{"sdd", "create"}, {"sdd", "transition"}, {"sdd-archive", "create"}, {"sdd-archive", "accept-revision"}} {
		var out, diagnostic bytes.Buffer
		code := RunProductSDDRuntime(context.Background(), args, strings.NewReader("{}"), &out, &diagnostic, nil, nil, nil, nil, nil, nil, nil)
		if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("%v: code=%d output=%q diagnostic=%q", args, code, out.String(), diagnostic.String())
		}
	}
}
