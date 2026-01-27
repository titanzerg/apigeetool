package cmd

import (
	"context"
	"encoding/json"
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

func resolveSyncDBURL(args SyncArgs) (string, error) {
	dbURL := strings.TrimSpace(args.DBURL)
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv("APIGEE_SYNC_DB_URL"))
	}
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dbURL == "" {
		return "", fmt.Errorf("sync requires PostgreSQL connection string via -sync-db-url, APIGEE_SYNC_DB_URL, or DATABASE_URL")
	}
	return dbURL, nil
}

func syncTableNames(args SyncArgs) (string, string, string, string, string, string, string) {
	table := DefaultString(args.EndpointsTable, "apigee.apigee_proxy_endpoints")
	targetEndpointsTable := DefaultString(args.TargetEndpointsTable, strings.TrimSpace(os.Getenv("APIGEE_SYNC_TARGET_ENDPOINT_TABLE")))
	targetEndpointsTable = DefaultString(targetEndpointsTable, "apigee.apigee_target_endpoints")
	targetTable := DefaultString(args.TargetTable, strings.TrimSpace(os.Getenv("APIGEE_SYNC_TARGET_TABLE")))
	targetTable = DefaultString(targetTable, "apigee.apigee_target_servers")
	productsTable := DefaultString(args.ProductsTable, "apigee.apigee_api_products")
	appsTable := DefaultString(args.AppsTable, strings.TrimSpace(os.Getenv("APIGEE_SYNC_APPS_TABLE")))
	appsTable = DefaultString(appsTable, "apigee.apigee_apps")
	appCredsTable := DefaultString(args.AppCredentialsTable, strings.TrimSpace(os.Getenv("APIGEE_SYNC_APP_CREDENTIALS_TABLE")))
	appCredsTable = DefaultString(appCredsTable, "apigee.apigee_app_credentials")
	proxyFlowsTable := DefaultString(args.ProxyFlowsTable, strings.TrimSpace(os.Getenv("APIGEE_SYNC_PROXY_FLOW_TABLE")))
	proxyFlowsTable = DefaultString(proxyFlowsTable, "apigee.apigee_proxy_endpoint_flows")
	return table, targetEndpointsTable, targetTable, productsTable, appsTable, appCredsTable, proxyFlowsTable
}

func syncDBOptions(args SyncArgs, dbURL string) dbConnOptions {
	return dbConnOptions{
		URL:            dbURL,
		RootCertPath:   FirstNonEmpty(args.SSLRoot, os.Getenv("APIGEE_SYNC_DB_SSL_ROOTCERT")),
		ClientCertPath: FirstNonEmpty(args.SSLCert, os.Getenv("APIGEE_SYNC_DB_SSL_CERT")),
		ClientKeyPath:  FirstNonEmpty(args.SSLKey, os.Getenv("APIGEE_SYNC_DB_SSL_KEY")),
	}
}

func openDBPool(ctx context.Context, opts dbConnOptions) (*pgxpool.Pool, error) {
	resolvedURL, err := buildDatabaseURL(opts)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, resolvedURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	return pool, nil
}

func collectProxyEndpointsIfNeeded(cfg ApigeeConfig, selection SyncSelection, progress func(apigee.ProxyScanProgress)) ([]apigee.ProxyEndpointRecord, error) {
	if !selection.needsProxyEndpoints() {
		return nil, nil
	}
	endpoints, err := apigee.CollectProxyEndpoints(apigee.CollectProxyEndpointsOptions{
		Host:     cfg.Host,
		Org:      cfg.Org,
		Token:    cfg.Token,
		Progress: progress,
	})
	if err != nil {
		return nil, fmt.Errorf("collect proxy endpoints: %w", err)
	}
	fmt.Printf("Fetched %d proxy endpoint(s) from Apigee\n", len(endpoints))
	return endpoints, nil
}

func syncProxyEndpointsIfRequested(ctx context.Context, pool *pgxpool.Pool, selection SyncSelection, table, targetEndpointsTable, proxyFlowsTable string, endpoints []apigee.ProxyEndpointRecord) error {
	if !selection.needsProxyEndpoints() {
		return nil
	}
	if selection.ProxyEndpoints {
		if err := syncProxyEndpoints(ctx, pool, table, endpoints); err != nil {
			return fmt.Errorf("sync proxy endpoints to PostgreSQL: %w", err)
		}
		fmt.Printf("Updated %d proxy endpoint(s) in %s\n", len(endpoints), table)
		if err := syncTargetEndpoints(ctx, pool, targetEndpointsTable, endpoints); err != nil {
			return fmt.Errorf("sync target endpoints to PostgreSQL: %w", err)
		}
		fmt.Printf("Updated target endpoint details in %s\n", targetEndpointsTable)
		if err := syncProxyFlowSteps(ctx, pool, proxyFlowsTable, endpoints); err != nil {
			return fmt.Errorf("sync proxy endpoint flow steps to PostgreSQL: %w", err)
		}
		fmt.Printf("Updated %d proxy endpoint flow record(s) in %s\n", len(endpoints), proxyFlowsTable)
	} else {
		fmt.Println("Skipped proxy endpoint sync (not requested)")
	}
	return nil
}

func syncTargetServersIfRequested(ctx context.Context, pool *pgxpool.Pool, selection SyncSelection, targetTable string, cfg ApigeeConfig, endpoints []apigee.ProxyEndpointRecord) error {
	if !selection.TargetServers {
		fmt.Println("Skipped target server sync (not requested)")
		return nil
	}
	tsRecords, err := apigee.CollectTargetServers(apigee.CollectTargetServersOptions{
		Host:      cfg.Host,
		Org:       cfg.Org,
		Token:     cfg.Token,
		Endpoints: endpoints,
		Progress:  targetServerProgressPrinter(),
	})
	if err != nil {
		return fmt.Errorf("collect target servers: %w", err)
	}
	fmt.Printf("Fetched %d target server(s) from Apigee\n", len(tsRecords))
	if err := syncTargetServers(ctx, pool, targetTable, tsRecords); err != nil {
		return fmt.Errorf("sync target servers to PostgreSQL: %w", err)
	}
	fmt.Printf("Updated %d target server(s) in %s\n", len(tsRecords), targetTable)
	return nil
}

func syncAPIProductsIfRequested(ctx context.Context, pool *pgxpool.Pool, selection SyncSelection, productsTable string, cfg ApigeeConfig) error {
	if !selection.APIProducts {
		fmt.Println("Skipped api product sync (not requested)")
		return nil
	}
	products, err := apigee.CollectAPIProducts(apigee.CollectAPIProductsOptions{
		Host:     cfg.Host,
		Org:      cfg.Org,
		Token:    cfg.Token,
		Progress: apiProductProgressPrinter(),
	})
	if err != nil {
		return fmt.Errorf("collect api products: %w", err)
	}
	fmt.Printf("Fetched %d api product(s) from Apigee\n", len(products))
	if err := syncAPIProducts(ctx, pool, productsTable, products); err != nil {
		return fmt.Errorf("sync api products to PostgreSQL: %w", err)
	}
	fmt.Printf("Updated %d api product(s) in %s\n", len(products), productsTable)
	return nil
}

func syncAppsIfRequested(ctx context.Context, pool *pgxpool.Pool, selection SyncSelection, appsTable, appCredentialsTable string, cfg ApigeeConfig) error {
	if !selection.Apps {
		fmt.Println("Skipped apps sync (not requested)")
		return nil
	}
	apps, creds, err := apigee.CollectApps(apigee.CollectAppsOptions{
		Host:     cfg.Host,
		Org:      cfg.Org,
		Token:    cfg.Token,
		Progress: appProgressPrinter(),
	})
	if err != nil {
		return fmt.Errorf("collect apps: %w", err)
	}
	fmt.Printf("Fetched %d app(s) and %d credential(s) from Apigee\n", len(apps), len(creds))
	if err := syncApps(ctx, pool, appsTable, appCredentialsTable, apps, creds); err != nil {
		return fmt.Errorf("sync apps to PostgreSQL: %w", err)
	}
	fmt.Printf("Updated %d app(s) in %s and %d credential(s) in %s\n", len(apps), appsTable, len(creds), appCredentialsTable)
	return nil
}

func syncProxyEndpoints(ctx context.Context, pool *pgxpool.Pool, table string, endpoints []apigee.ProxyEndpointRecord) error {
	return withTx(ctx, pool, table, func(ctx context.Context, tx pgx.Tx, quotedTable string) error {
		if _, err := tx.Exec(ctx, deleteFromPrefix+quotedTable); err != nil {
			return fmt.Errorf(clearTableFmt, quotedTable, err)
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (proxy_name, endpoint_name, revision, base_path, target_servers, environments, flow_count, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
			quotedTable,
		)
		snapshot := time.Now().UTC()
		for _, ep := range endpoints {
			servers := normalizeStringSlice(ep.Targets)
			envs := normalizeStringSlice(ep.Envs)
			if _, err := tx.Exec(ctx, insertSQL, ep.Proxy, ep.Endpoint, ep.Revision, ep.BasePath, servers, envs, ep.Flows, snapshot); err != nil {
				return fmt.Errorf("insert %s.%s: %w", ep.Proxy, ep.Endpoint, err)
			}
		}
		return nil
	})
}

func syncProxyFlowSteps(ctx context.Context, pool *pgxpool.Pool, table string, endpoints []apigee.ProxyEndpointRecord) error {
	return withTx(ctx, pool, table, func(ctx context.Context, tx pgx.Tx, quotedTable string) error {
		if _, err := tx.Exec(ctx, deleteFromPrefix+quotedTable); err != nil {
			return fmt.Errorf(clearTableFmt, quotedTable, err)
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (proxy_name, endpoint_name, preflow_request_steps, preflow_response_steps, postflow_request_steps, postflow_response_steps, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)",
			quotedTable,
		)
		now := time.Now().UTC()
		for _, ep := range endpoints {
			preReq := normalizeStringSlice(ep.FlowSteps.PreFlowRequest)
			preResp := normalizeStringSlice(ep.FlowSteps.PreFlowResponse)
			postReq := normalizeStringSlice(ep.FlowSteps.PostFlowRequest)
			postResp := normalizeStringSlice(ep.FlowSteps.PostFlowResponse)
			if _, err := tx.Exec(ctx, insertSQL, ep.Proxy, ep.Endpoint, preReq, preResp, postReq, postResp, now); err != nil {
				return fmt.Errorf("insert proxy flow steps %s/%s: %w", ep.Proxy, ep.Endpoint, err)
			}
		}
		return nil
	})
}

func syncTargetEndpoints(ctx context.Context, pool *pgxpool.Pool, table string, endpoints []apigee.ProxyEndpointRecord) error {
	return withTx(ctx, pool, table, func(ctx context.Context, tx pgx.Tx, quotedTable string) error {
		if _, err := tx.Exec(ctx, deleteFromPrefix+quotedTable); err != nil {
			return fmt.Errorf(clearTableFmt, quotedTable, err)
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (proxy_name, endpoint_name, target_endpoint_name, target_url, load_balancer_servers, properties, success_codes, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
			quotedTable,
		)
		snapshot := time.Now().UTC()
		for _, ep := range endpoints {
			for _, target := range ep.TargetEndpoints {
				props := target.Properties
				if props == nil {
					props = map[string]string{}
				}
				propsJSON, err := json.Marshal(props)
				if err != nil {
					return fmt.Errorf("marshal target endpoint properties %s/%s/%s: %w", ep.Proxy, ep.Endpoint, target.Name, err)
				}
				servers := normalizeStringSlice(target.LoadBalancer)
				url := strings.TrimSpace(target.URL)
				success := strings.TrimSpace(target.SuccessCodes)
				if _, err := tx.Exec(ctx, insertSQL, ep.Proxy, ep.Endpoint, target.Name, url, servers, propsJSON, success, snapshot); err != nil {
					return fmt.Errorf("insert target endpoint %s/%s/%s: %w", ep.Proxy, ep.Endpoint, target.Name, err)
				}
			}
		}
		return nil
	})
}

func syncTargetServers(ctx context.Context, pool *pgxpool.Pool, table string, servers []apigee.TargetServerRecord) error {
	return withTx(ctx, pool, table, func(ctx context.Context, tx pgx.Tx, quotedTable string) error {
		if _, err := tx.Exec(ctx, deleteFromPrefix+quotedTable); err != nil {
			return fmt.Errorf(clearTableFmt, quotedTable, err)
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
		if _, err := tx.Exec(ctx, deleteFromPrefix+quotedTable); err != nil {
			return fmt.Errorf(clearTableFmt, quotedTable, err)
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (name, environments, apiproxies, apps, updated_at) VALUES ($1, $2, $3, $4, $5)",
			quotedTable,
		)
		now := time.Now().UTC()
		for _, prod := range products {
			envs := normalizeStringSlice(prod.Environments)
			proxies := normalizeStringSlice(prod.Proxies)
			apps := normalizeStringSlice(prod.Apps)
			if _, err := tx.Exec(ctx, insertSQL, prod.Name, envs, proxies, apps, now); err != nil {
				return fmt.Errorf("insert api product %s: %w", prod.Name, err)
			}
		}
		return nil
	})
}

func syncApps(ctx context.Context, pool *pgxpool.Pool, appsTable, appCredentialsTable string, apps []apigee.AppRecord, creds []apigee.AppCredentialRecord) error {
	quotedAppsTable, err := quoteTableName(appsTable)
	if err != nil {
		return err
	}
	quotedCredsTable, err := quoteTableName(appCredentialsTable)
	if err != nil {
		return err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, deleteFromPrefix+quotedCredsTable); err != nil {
		return fmt.Errorf(clearTableFmt, quotedCredsTable, err)
	}
	if _, err := tx.Exec(ctx, deleteFromPrefix+quotedAppsTable); err != nil {
		return fmt.Errorf(clearTableFmt, quotedAppsTable, err)
	}

	appInsertSQL := fmt.Sprintf(
		"INSERT INTO %s (app_id, name, owner, registered_at, notes, updated_at) VALUES ($1, $2, $3, $4, $5, $6)",
		quotedAppsTable,
	)
	credInsertSQL := fmt.Sprintf(
		"INSERT INTO %s (app_id, consumer_key, consumer_secret, expires_at, products, updated_at) VALUES ($1, $2, $3, $4, $5, $6)",
		quotedCredsTable,
	)
	now := time.Now().UTC()
	for _, app := range apps {
		if _, err := tx.Exec(ctx, appInsertSQL, app.AppID, app.Name, app.Owner, app.RegisteredAt, app.Notes, now); err != nil {
			return fmt.Errorf("insert app %s: %w", app.AppID, err)
		}
	}
	for _, cred := range creds {
		products := normalizeStringSlice(cred.Products)
		if _, err := tx.Exec(ctx, credInsertSQL, cred.AppID, cred.Key, cred.Secret, cred.ExpiresAt, products, now); err != nil {
			return fmt.Errorf("insert app credential %s/%s: %w", cred.AppID, cred.Key, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func normalizeStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
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
