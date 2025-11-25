package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"apigee/internal/apigee"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RunFindProxy(cfg ApigeeConfig, args FindArgs) error {
	searchTerm := normalizeBasePathLocal(args.BasePath)
	if searchTerm == "" {
		return fmt.Errorf("base path is required for -findproxy")
	}

	if dbURL := firstDBURL(args.DBURL); dbURL != "" {
		matches, err := findProxiesInDB(searchTerm, dbURL, args)
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			fmt.Printf("No Apigee proxies found in DB with BasePath containing %q\n", searchTerm)
			return nil
		}

		fmt.Printf("Found %d Apigee proxies with BasePath containing %q (from DB):\n", len(matches), searchTerm)
		for _, match := range matches {
			fmt.Printf("- %s (endpoint %s, revision %d, basepath %s)\n", match.Proxy, match.Endpoint, match.Revision, match.BasePath)
		}
		return nil
	}

	if err := RequireApigeeAuth(cfg, "finding proxies"); err != nil {
		return err
	}
	progress := func(p apigee.ProxyScanProgress) {
		if p.Err != nil {
			fmt.Printf("[%d/%d] %s (rev %d) error: %v\n", p.Index, p.Total, p.Proxy, p.Revision, p.Err)
			return
		}
		basePaths := "<none>"
		if len(p.BasePaths) > 0 {
			basePaths = strings.Join(p.BasePaths, ", ")
		}
		fmt.Printf("[%d/%d] %s (rev %d) basepaths: %s\n", p.Index, p.Total, p.Proxy, p.Revision, basePaths)
		if p.Matched {
			fmt.Println("  -> MATCH")
		} else {
			fmt.Println("  -> no match")
		}
	}
	matches, err := apigee.FindProxiesByBasePath(apigee.FindProxyOptions{
		Host:     cfg.Host,
		Org:      cfg.Org,
		Token:    cfg.Token,
		BasePath: searchTerm,
		Progress: progress,
	})
	if err != nil {
		return fmt.Errorf("find proxies by base path: %w", err)
	}
	if len(matches) == 0 {
		fmt.Printf("No Apigee proxies found with BasePath containing %q\n", searchTerm)
		return nil
	}

	fmt.Printf("Found %d Apigee proxies with BasePath containing %q:\n", len(matches), searchTerm)
	for _, match := range matches {
		fmt.Printf("- %s (endpoint %s, revision %d, basepath %s)\n", match.Proxy, match.Endpoint, match.Revision, match.BasePath)
	}
	return nil
}

func findProxiesInDB(basePath, dbURL string, args FindArgs) ([]apigee.ProxyMatch, error) {
	table := DefaultString(args.Table, "apigee.apigee_proxy_endpoints")
	dbOpts := dbConnOptions{
		URL:            dbURL,
		RootCertPath:   FirstNonEmpty(args.SSLRoot, os.Getenv("APIGEE_SYNC_DB_SSL_ROOTCERT")),
		ClientCertPath: FirstNonEmpty(args.SSLCert, os.Getenv("APIGEE_SYNC_DB_SSL_CERT")),
		ClientKeyPath:  FirstNonEmpty(args.SSLKey, os.Getenv("APIGEE_SYNC_DB_SSL_KEY")),
	}

	ctx := context.Background()
	resolvedURL, err := buildDatabaseURL(dbOpts)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, resolvedURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()

	quotedTable, err := quoteTableName(table)
	if err != nil {
		return nil, err
	}

	base := normalizeBasePathLocal(basePath)
	if base == "" {
		return nil, fmt.Errorf("base path is required")
	}

	pattern := "%" + escapeLike(base) + "%"

	query := fmt.Sprintf(
		`SELECT proxy_name, endpoint_name, revision, base_path FROM %s WHERE base_path ILIKE $1 ESCAPE '\'`,
		quotedTable,
	)
	rows, err := pool.Query(ctx, query, pattern)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	var matches []apigee.ProxyMatch
	for rows.Next() {
		var m apigee.ProxyMatch
		if err := rows.Scan(&m.Proxy, &m.Endpoint, &m.Revision, &m.BasePath); err != nil {
			return nil, fmt.Errorf("scan %s: %w", table, err)
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", table, err)
	}
	return matches, nil
}

func normalizeBasePathLocal(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func firstDBURL(candidate string) string {
	if trimmed := strings.TrimSpace(candidate); trimmed != "" {
		return trimmed
	}
	if env := strings.TrimSpace(os.Getenv("APIGEE_SYNC_DB_URL")); env != "" {
		return env
	}
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}

func escapeLike(term string) string {
	term = strings.TrimSpace(term)
	term = strings.ReplaceAll(term, `\`, `\\`)
	term = strings.ReplaceAll(term, "%", `\%`)
	term = strings.ReplaceAll(term, "_", `\_`)
	return term
}
