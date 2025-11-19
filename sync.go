package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"Apigee/internal/apigee"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func syncProxyEndpoints(ctx context.Context, dbURL, table string, endpoints []apigee.ProxyEndpointRecord) error {
	if strings.TrimSpace(dbURL) == "" {
		return fmt.Errorf("database URL is required")
	}
	quotedTable, err := quoteTableName(table)
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "DELETE FROM "+quotedTable); err != nil {
		return fmt.Errorf("clear %s: %w", quotedTable, err)
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (proxy_name, endpoint_name, revision, base_path, updated_at) VALUES ($1, $2, $3, $4, $5)",
		quotedTable,
	)
	snapshot := time.Now().UTC()
	for _, ep := range endpoints {
		if _, err := tx.Exec(ctx, insertSQL, ep.Proxy, ep.Endpoint, ep.Revision, ep.BasePath, snapshot); err != nil {
			return fmt.Errorf("insert %s.%s: %w", ep.Proxy, ep.Endpoint, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func quoteTableName(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return "", fmt.Errorf("table name is required")
	}
	parts := strings.Split(table, ".")
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, `"`) {
			return "", fmt.Errorf("invalid identifier component %q", part)
		}
		quoted = append(quoted, `"`+part+`"`)
	}
	if len(quoted) == 0 {
		return "", fmt.Errorf("table name %q is invalid", table)
	}
	return strings.Join(quoted, "."), nil
}
