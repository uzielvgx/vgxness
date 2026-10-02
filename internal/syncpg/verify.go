package syncpg

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func recoverySchema(ctx context.Context, tx pgx.Tx) (string, error) {
	var schemas []string
	if err := tx.QueryRow(ctx, "SELECT current_schemas(false)").Scan(&schemas); err != nil || len(schemas) != 1 {
		return "", errors.New("recovery schema")
	}
	schema := schemas[0]
	if schema == "" || schema == "pg_catalog" || strings.HasPrefix(schema, "pg_temp") {
		return "", errors.New("recovery schema")
	}
	return schema, nil
}
