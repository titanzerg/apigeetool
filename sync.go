package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"Apigee/internal/apigee"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dbConnOptions struct {
	URL            string
	RootCertPath   string
	ClientCertPath string
	ClientKeyPath  string
}

func syncProxyEndpoints(ctx context.Context, dbOpts dbConnOptions, table string, endpoints []apigee.ProxyEndpointRecord) error {
	quotedTable, err := quoteTableName(table)
	if err != nil {
		return err
	}

	dbURL, err := buildDatabaseURL(dbOpts)
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

func buildDatabaseURL(opts dbConnOptions) (string, error) {
	raw := strings.TrimSpace(opts.URL)
	if raw == "" {
		return "", fmt.Errorf("database URL is required")
	}
	raw = sanitizeDSN(raw)

	for _, f := range []struct {
		label string
		path  string
	}{
		{"CA certificate", opts.RootCertPath},
		{"client certificate", opts.ClientCertPath},
		{"client key", opts.ClientKeyPath},
	} {
		if f.path == "" {
			continue
		}
		if err := ensureReadableFile(f.path); err != nil {
			return "", fmt.Errorf("%s %s: %w", f.label, f.path, err)
		}
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse database URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("database URL must include scheme and host (e.g. postgres://user:pass@host/db)")
	}

	q := u.Query()
	enforceSSLMode(q)
	if opts.RootCertPath != "" {
		q.Set("sslrootcert", opts.RootCertPath)
	}
	if opts.ClientCertPath != "" {
		q.Set("sslcert", opts.ClientCertPath)
	}
	if opts.ClientKeyPath != "" {
		q.Set("sslkey", opts.ClientKeyPath)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func enforceSSLMode(q url.Values) {
	mode := strings.ToLower(strings.TrimSpace(q.Get("sslmode")))
	switch mode {
	case "", "disable", "allow", "prefer":
		q.Set("sslmode", "require")
	}
}

func ensureReadableFile(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}

func sanitizeDSN(raw string) string {
	if !strings.Contains(raw, "%") {
		return raw
	}
	var builder strings.Builder
	builder.Grow(len(raw) + 4)
	for i := 0; i < len(raw); i++ {
		if raw[i] == '%' {
			if i+2 >= len(raw) || !isHex(raw[i+1]) || !isHex(raw[i+2]) {
				builder.WriteString("%25")
				continue
			}
		}
		builder.WriteByte(raw[i])
	}
	return builder.String()
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'f') ||
		(b >= 'A' && b <= 'F')
}
