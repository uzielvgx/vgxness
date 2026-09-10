package memory

import (
	"context"
	"errors"
	"fmt"

	"modernc.org/sqlite"
)

// sqliteDiagnostic deliberately recognizes only the driver's typed error. It
// never copies an arbitrary driver message, which can contain SQL or paths.
func sqliteDiagnostic(err error) string {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return ""
	}
	code := int(sqliteErr.Code()) & 0xff
	category := "other"
	switch code {
	case 5:
		category = "busy"
	case 6:
		category = "locked"
	case 14:
		category = "cantopen"
	case 11:
		category = "corrupt"
	}
	return fmt.Sprintf(" sqlite=%s code=%d", category, code)
}

func configurationError(ctx context.Context, stage string, err error) error {
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	return fmt.Errorf("configure memory store: %w: stage=%s%s", ErrCorrupt, stage, sqliteDiagnostic(err))
}
