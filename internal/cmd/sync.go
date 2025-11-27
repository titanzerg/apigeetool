package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"apigee/internal/apigee"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dbConnOptions struct {
	URL            string
	RootCertPath   string
	ClientCertPath string
	ClientKeyPath  string
}

// SyncSelection defines which resources should be synchronized.
type SyncSelection struct {
	ProxyEndpoints bool
	TargetServers  bool
	APIProducts    bool
}

// Any reports whether at least one sync target is enabled.
func (s SyncSelection) Any() bool {
	return s.ProxyEndpoints || s.TargetServers || s.APIProducts
}

// needsProxyEndpoints reports whether proxy endpoints must be fetched to support the requested sync.
func (s SyncSelection) needsProxyEndpoints() bool {
	return s.ProxyEndpoints || s.TargetServers
}

// ParseSyncSelection turns a comma-separated flag value into a SyncSelection.
func ParseSyncSelection(raw string) (SyncSelection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SyncSelection{}, fmt.Errorf("sync requires at least one target: apiproxy, target_server, api_product, or all")
	}

	var selection SyncSelection
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		token := normalizeSyncToken(part)
		switch token {
		case "all":
			selection.ProxyEndpoints = true
			selection.TargetServers = true
			selection.APIProducts = true
		case "apiproxy", "apiproxies", "proxy", "proxies":
			selection.ProxyEndpoints = true
		case "target_server", "targetserver", "targetservers", "targets", "target":
			selection.TargetServers = true
		case "api_product", "apiproduct", "api_products", "apiproducts", "product", "products":
			selection.APIProducts = true
		default:
			if token != "" {
				return SyncSelection{}, fmt.Errorf("unknown sync target %q (valid values: all, apiproxy, target_server, api_product)", part)
			}
		}
	}

	if !selection.Any() {
		return SyncSelection{}, fmt.Errorf("sync requires at least one target: apiproxy, target_server, api_product, or all")
	}
	return selection, nil
}

func normalizeSyncToken(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	token = strings.ReplaceAll(token, "-", "_")
	return token
}

func RunSync(cfg ApigeeConfig, args SyncArgs) error {
	if err := RequireApigeeAuth(cfg, "sync"); err != nil {
		return err
	}

	selection := args.Selection
	if !selection.Any() {
		return fmt.Errorf("sync requires at least one target: apiproxy, target_server, api_product, or all")
	}

	dbURL := strings.TrimSpace(args.DBURL)
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv("APIGEE_SYNC_DB_URL"))
	}
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dbURL == "" {
		return fmt.Errorf("sync requires PostgreSQL connection string via -sync-db-url, APIGEE_SYNC_DB_URL, or DATABASE_URL")
	}

	table := DefaultString(args.EndpointsTable, "apigee.apigee_proxy_endpoints")
	targetTable := DefaultString(args.TargetTable, strings.TrimSpace(os.Getenv("APIGEE_SYNC_TARGET_TABLE")))
	targetTable = DefaultString(targetTable, "apigee.apigee_target_servers")
	productsTable := DefaultString(args.ProductsTable, "apigee.apigee_api_products")

	progress := func(p apigee.ProxyScanProgress) {
		if p.Err != nil {
			fmt.Printf("[%d/%d] %s (rev %d) error: %v\n", p.Index, p.Total, p.Proxy, p.Revision, p.Err)
			return
		}
		basePaths := "<none>"
		if len(p.BasePaths) > 0 {
			basePaths = strings.Join(p.BasePaths, ", ")
		}
		envs := "<none>"
		if len(p.Envs) > 0 {
			envs = strings.Join(p.Envs, ", ")
		}
		if p.EnvError != "" {
			envs = fmt.Sprintf("%s (env error: %s)", envs, p.EnvError)
		}
		fmt.Printf("[%d/%d] %s (rev %d) basepaths: %s envs: %s\n", p.Index, p.Total, p.Proxy, p.Revision, basePaths, envs)
	}

	dbOpts := dbConnOptions{
		URL:            dbURL,
		RootCertPath:   FirstNonEmpty(args.SSLRoot, os.Getenv("APIGEE_SYNC_DB_SSL_ROOTCERT")),
		ClientCertPath: FirstNonEmpty(args.SSLCert, os.Getenv("APIGEE_SYNC_DB_SSL_CERT")),
		ClientKeyPath:  FirstNonEmpty(args.SSLKey, os.Getenv("APIGEE_SYNC_DB_SSL_KEY")),
	}

	ctx := context.Background()
	resolvedURL, err := buildDatabaseURL(dbOpts)
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, resolvedURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()

	var endpoints []apigee.ProxyEndpointRecord
	if selection.needsProxyEndpoints() {
		var err error
		endpoints, err = apigee.CollectProxyEndpoints(apigee.CollectProxyEndpointsOptions{
			Host:     cfg.Host,
			Org:      cfg.Org,
			Token:    cfg.Token,
			Progress: progress,
		})
		if err != nil {
			return fmt.Errorf("collect proxy endpoints: %w", err)
		}
		fmt.Printf("Fetched %d proxy endpoint(s) from Apigee\n", len(endpoints))

		if selection.ProxyEndpoints {
			if err := syncProxyEndpoints(ctx, pool, table, endpoints); err != nil {
				return fmt.Errorf("sync proxy endpoints to PostgreSQL: %w", err)
			}
			fmt.Printf("Updated %d proxy endpoint(s) in %s\n", len(endpoints), table)
		} else {
			fmt.Println("Skipped proxy endpoint sync (not requested)")
		}
	}

	if selection.TargetServers {
		tsRecords, err := apigee.CollectTargetServers(apigee.CollectTargetServersOptions{
			Host:      cfg.Host,
			Org:       cfg.Org,
			Token:     cfg.Token,
			Endpoints: endpoints,
			Progress: func(p apigee.TargetServerProgress) {
				if p.Total == 0 {
					return
				}
				url := strings.TrimSpace(p.URL)
				if url == "" {
					url = "<unknown>"
				}
				if p.Err != nil {
					fmt.Printf("[%d/%d] target %s/%s url: %s error: %v\n", p.Index, p.Total, p.Environment, p.Name, url, p.Err)
					return
				}
				fmt.Printf("[%d/%d] target %s/%s url: %s\n", p.Index, p.Total, p.Environment, p.Name, url)
			},
		})
		if err != nil {
			return fmt.Errorf("collect target servers: %w", err)
		}
		fmt.Printf("Fetched %d target server(s) from Apigee\n", len(tsRecords))
		if err := syncTargetServers(ctx, pool, targetTable, tsRecords); err != nil {
			return fmt.Errorf("sync target servers to PostgreSQL: %w", err)
		}
		fmt.Printf("Updated %d target server(s) in %s\n", len(tsRecords), targetTable)
	} else {
		fmt.Println("Skipped target server sync (not requested)")
	}

	if selection.APIProducts {
		products, err := apigee.CollectAPIProducts(apigee.CollectAPIProductsOptions{
			Host:  cfg.Host,
			Org:   cfg.Org,
			Token: cfg.Token,
			Progress: func(p apigee.APIProductProgress) {
				envs := "<none>"
				if len(p.Environments) > 0 {
					envs = strings.Join(p.Environments, ", ")
				}
				proxies := "<none>"
				if len(p.Proxies) > 0 {
					proxies = strings.Join(p.Proxies, ", ")
				}
				if p.Err != nil {
					fmt.Printf("[%d/%d] product %s envs: %s proxies: %s apps: %d error: %v\n", p.Index, p.Total, p.Name, envs, proxies, p.Apps, p.Err)
					return
				}
				fmt.Printf("[%d/%d] product %s envs: %s proxies: %s apps: %d\n", p.Index, p.Total, p.Name, envs, proxies, p.Apps)
			},
		})
		if err != nil {
			return fmt.Errorf("collect api products: %w", err)
		}
		fmt.Printf("Fetched %d api product(s) from Apigee\n", len(products))
		if err := syncAPIProducts(ctx, pool, productsTable, products); err != nil {
			return fmt.Errorf("sync api products to PostgreSQL: %w", err)
		}
		fmt.Printf("Updated %d api product(s) in %s\n", len(products), productsTable)
	} else {
		fmt.Println("Skipped api product sync (not requested)")
	}

	return nil
}

func syncProxyEndpoints(ctx context.Context, pool *pgxpool.Pool, table string, endpoints []apigee.ProxyEndpointRecord) error {
	return withTx(ctx, pool, table, func(ctx context.Context, tx pgx.Tx, quotedTable string) error {
		if _, err := tx.Exec(ctx, "DELETE FROM "+quotedTable); err != nil {
			return fmt.Errorf("clear %s: %w", quotedTable, err)
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (proxy_name, endpoint_name, revision, base_path, target_servers, environments, flow_count, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
			quotedTable,
		)
		snapshot := time.Now().UTC()
		for _, ep := range endpoints {
			servers := ep.Targets
			if servers == nil {
				servers = []string{}
			}
			envs := ep.Envs
			if envs == nil {
				envs = []string{}
			}
			if _, err := tx.Exec(ctx, insertSQL, ep.Proxy, ep.Endpoint, ep.Revision, ep.BasePath, servers, envs, ep.Flows, snapshot); err != nil {
				return fmt.Errorf("insert %s.%s: %w", ep.Proxy, ep.Endpoint, err)
			}
		}
		return nil
	})
}

func syncTargetServers(ctx context.Context, pool *pgxpool.Pool, table string, servers []apigee.TargetServerRecord) error {
	return withTx(ctx, pool, table, func(ctx context.Context, tx pgx.Tx, quotedTable string) error {
		if _, err := tx.Exec(ctx, "DELETE FROM "+quotedTable); err != nil {
			return fmt.Errorf("clear %s: %w", quotedTable, err)
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (name, environment, url, host, port, is_ssl, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)",
			quotedTable,
		)
		now := time.Now().UTC()
		for _, srv := range servers {
			if _, err := tx.Exec(ctx, insertSQL, srv.Name, srv.Environment, srv.URL, srv.Host, srv.Port, srv.IsSSL, now); err != nil {
				return fmt.Errorf("insert target server %s/%s: %w", srv.Environment, srv.Name, err)
			}
		}
		return nil
	})
}

func syncAPIProducts(ctx context.Context, pool *pgxpool.Pool, table string, products []apigee.APIProductRecord) error {
	return withTx(ctx, pool, table, func(ctx context.Context, tx pgx.Tx, quotedTable string) error {
		if _, err := tx.Exec(ctx, "DELETE FROM "+quotedTable); err != nil {
			return fmt.Errorf("clear %s: %w", quotedTable, err)
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (name, environments, apiproxies, apps, updated_at) VALUES ($1, $2, $3, $4, $5)",
			quotedTable,
		)
		now := time.Now().UTC()
		for _, prod := range products {
			envs := prod.Environments
			if envs == nil {
				envs = []string{}
			}
			proxies := prod.Proxies
			if proxies == nil {
				proxies = []string{}
			}
			apps := prod.Apps
			if apps == nil {
				apps = []string{}
			}
			if _, err := tx.Exec(ctx, insertSQL, prod.Name, envs, proxies, apps, now); err != nil {
				return fmt.Errorf("insert api product %s: %w", prod.Name, err)
			}
		}
		return nil
	})
}

type syncExec func(ctx context.Context, tx pgx.Tx, quotedTable string) error

func withTx(ctx context.Context, pool *pgxpool.Pool, table string, exec syncExec) error {
	quotedTable, err := quoteTableName(table)
	if err != nil {
		return err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := exec(ctx, tx, quotedTable); err != nil {
		return err
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
