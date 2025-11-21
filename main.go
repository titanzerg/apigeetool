package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"Apigee/internal/apigee"
	"Apigee/internal/openapi"
	"Apigee/internal/proxyxml"
	"Apigee/internal/report"
	"Apigee/internal/update"
)

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	var (
		inputPath   = flag.String("input", "openapi.yaml", "path to the OpenAPI v3 file")
		outputPath  = flag.String("output", "proxy-endpoint.xml", "output path for the Apigee ProxyEndpoint XML")
		name        = flag.String("name", "default", "ProxyEndpoint name attribute")
		basePath    = flag.String("basepath", "", "HTTPProxyConnection BasePath (defaults to slugified info.title)")
		proxyName   = flag.String("proxy", "", "Apigee API proxy name to download ProxyEndpoint XML files")
		apigeeOrg   = flag.String("org", "", "Apigee organization to use with -proxy (defaults to APIGEE_ORG)")
		revision    = flag.Int("revision", 0, "Specific revision to download from Apigee (defaults to latest)")
		apigeeHost  = flag.String("apigee-host", "https://apigee.googleapis.com", "Apigee management API base URL")
		downloadDir = flag.String(
			"download-dir",
			"downloaded-proxy-endpoints",
			"Destination directory for downloaded ProxyEndpoint XML files",
		)
		apigeeToken        = flag.String("token", "", "Apigee OAuth token (defaults to APIGEE_TOKEN env var)")
		findBase           = flag.String("findproxy", "", "Find Apigee proxies that use the specified BasePath")
		syncFlag           = flag.Bool("sync", false, "Sync all Apigee proxy endpoints into PostgreSQL")
		syncDBURL          = flag.String("sync-db-url", "", "PostgreSQL connection URL (defaults to APIGEE_SYNC_DB_URL or DATABASE_URL)")
		syncEndpointsTable = flag.String("sync-endpoints-table", "apigee.apigee_proxy_endpoints", "Target PostgreSQL table for -sync (endpoints)")
		syncTargetTable    = flag.String("sync-target-table", "apigee.apigee_target_servers", "Target PostgreSQL table for target servers (sync mode)")
		syncSSLRoot        = flag.String("sync-ssl-rootcert", "", "Path to CA certificate for -sync DB connection (defaults to APIGEE_SYNC_DB_SSL_ROOTCERT)")
		syncSSLCert        = flag.String("sync-ssl-cert", "", "Path to client certificate for -sync DB connection (defaults to APIGEE_SYNC_DB_SSL_CERT)")
		syncSSLKey         = flag.String("sync-ssl-key", "", "Path to client key for -sync DB connection (defaults to APIGEE_SYNC_DB_SSL_KEY)")
		syncProductsTable  = flag.String("sync-products-table", "apigee.apigee_api_products", "Target PostgreSQL table for API products (sync mode)")
	)

	flag.Parse()

	if targetBase := strings.TrimSpace(*findBase); targetBase != "" {
		org, token, host := resolveApigeeConfig(*apigeeOrg, *apigeeToken, *apigeeHost)
		if org == "" || token == "" {
			log.Fatal("finding proxies requires Apigee org (-org or APIGEE_ORG) and token (-token or APIGEE_TOKEN)")
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
			Host:     host,
			Org:      org,
			Token:    token,
			BasePath: targetBase,
			Progress: progress,
		})
		if err != nil {
			log.Fatalf("find proxies by base path: %v", err)
		}
		if len(matches) == 0 {
			fmt.Printf("No Apigee proxies found with BasePath %s\n", targetBase)
		} else {
			fmt.Printf("Found %d Apigee proxies with BasePath %s:\n", len(matches), targetBase)
			for _, match := range matches {
				fmt.Printf("- %s (endpoint %s, revision %d)\n", match.Proxy, match.Endpoint, match.Revision)
			}
		}
		return
	}

	if *syncFlag {
		org, token, host := resolveApigeeConfig(*apigeeOrg, *apigeeToken, *apigeeHost)
		if org == "" || token == "" {
			log.Fatal("sync requires Apigee org (-org or APIGEE_ORG) and token (-token or APIGEE_TOKEN)")
		}

		dbURL := strings.TrimSpace(*syncDBURL)
		if dbURL == "" {
			dbURL = strings.TrimSpace(os.Getenv("APIGEE_SYNC_DB_URL"))
		}
		if dbURL == "" {
			dbURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
		}
		if dbURL == "" {
			log.Fatal("sync requires PostgreSQL connection string via -sync-db-url, APIGEE_SYNC_DB_URL, or DATABASE_URL")
		}

		table := strings.TrimSpace(*syncEndpointsTable)
		if table == "" {
			table = "apigee.apigee_proxy_endpoints"
		}
		targetTable := strings.TrimSpace(*syncTargetTable)
		if targetTable == "" {
			targetTable = strings.TrimSpace(os.Getenv("APIGEE_SYNC_TARGET_TABLE"))
		}
		if targetTable == "" {
			targetTable = "apigee.apigee_target_servers"
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
			envs := "<none>"
			if len(p.Envs) > 0 {
				envs = strings.Join(p.Envs, ", ")
			}
			if p.EnvError != "" {
				envs = fmt.Sprintf("%s (env error: %s)", envs, p.EnvError)
			}
			fmt.Printf("[%d/%d] %s (rev %d) basepaths: %s envs: %s\n", p.Index, p.Total, p.Proxy, p.Revision, basePaths, envs)
		}

		endpoints, err := apigee.CollectProxyEndpoints(apigee.CollectProxyEndpointsOptions{
			Host:     host,
			Org:      org,
			Token:    token,
			Progress: progress,
		})
		if err != nil {
			log.Fatalf("collect proxy endpoints: %v", err)
		}
		fmt.Printf("Fetched %d proxy endpoint(s) from Apigee\n", len(endpoints))

		rootCert := firstNonEmpty(*syncSSLRoot, os.Getenv("APIGEE_SYNC_DB_SSL_ROOTCERT"))
		clientCert := firstNonEmpty(*syncSSLCert, os.Getenv("APIGEE_SYNC_DB_SSL_CERT"))
		clientKey := firstNonEmpty(*syncSSLKey, os.Getenv("APIGEE_SYNC_DB_SSL_KEY"))

		dbOpts := dbConnOptions{
			URL:            dbURL,
			RootCertPath:   rootCert,
			ClientCertPath: clientCert,
			ClientKeyPath:  clientKey,
		}

		if err := syncProxyEndpoints(context.Background(), dbOpts, table, endpoints); err != nil {
			log.Fatalf("sync proxy endpoints to PostgreSQL: %v", err)
		}
		fmt.Printf("Updated %d proxy endpoint(s) in %s\n", len(endpoints), table)

		tsRecords, err := apigee.CollectTargetServers(apigee.CollectTargetServersOptions{
			Host:      host,
			Org:       org,
			Token:     token,
			Endpoints: endpoints,
		})
		if err != nil {
			log.Fatalf("collect target servers: %v", err)
		}
		fmt.Printf("Fetched %d target server(s) from Apigee\n", len(tsRecords))
		if err := syncTargetServers(context.Background(), dbOpts, targetTable, tsRecords); err != nil {
			log.Fatalf("sync target servers to PostgreSQL: %v", err)
		}
		fmt.Printf("Updated %d target server(s) in %s\n", len(tsRecords), targetTable)

		products, err := apigee.CollectAPIProducts(apigee.CollectAPIProductsOptions{
			Host:  host,
			Org:   org,
			Token: token,
		})
		if err != nil {
			log.Fatalf("collect api products: %v", err)
		}
		fmt.Printf("Fetched %d api product(s) from Apigee\n", len(products))
		if err := syncAPIProducts(context.Background(), dbOpts, strings.TrimSpace(*syncProductsTable), products); err != nil {
			log.Fatalf("sync api products to PostgreSQL: %v", err)
		}
		fmt.Printf("Updated %d api product(s) in %s\n", len(products), strings.TrimSpace(*syncProductsTable))
		return
	}

	data, err := os.ReadFile(*inputPath)
	if err != nil {
		log.Fatalf("read OpenAPI document: %v", err)
	}

	spec, err := openapi.Parse(data)
	if err != nil {
		log.Fatalf("parse OpenAPI YAML: %v", err)
	}

	if len(spec.Paths) == 0 {
		log.Fatal("no paths defined in OpenAPI document")
	}

	ordering := openapi.ExtractPathOrdering(data)
	flows := openapi.BuildFlows(spec.Paths, ordering)
	if len(flows) == 0 {
		log.Fatal("no operations found under OpenAPI paths")
	}

	path := strings.TrimSpace(*basePath)
	if path == "" {
		path = openapi.SlugifyTitle(spec.Info.Title)
	}

	xml := openapi.RenderProxyEndpoint(*name, path, flows)

	if err := os.WriteFile(*outputPath, []byte(xml), 0o644); err != nil {
		log.Fatalf("write output XML: %v", err)
	}

	fmt.Printf("Generated %d flows at %s\n", len(flows), *outputPath)

	if proxy := strings.TrimSpace(*proxyName); proxy != "" {
		org, token, host := resolveApigeeConfig(*apigeeOrg, *apigeeToken, *apigeeHost)
		if org == "" || token == "" {
			log.Fatal("downloading proxies requires Apigee org (-org or APIGEE_ORG) and token (-token or APIGEE_TOKEN)")
		}
		downloadPath := strings.TrimSpace(*downloadDir)
		if downloadPath == "" {
			downloadPath = "downloaded-proxy-endpoints"
		}

		if err := os.RemoveAll(downloadPath); err != nil {
			log.Fatalf("cleanup download directory: %v", err)
		}
		if err := os.MkdirAll(downloadPath, 0o755); err != nil {
			log.Fatalf("create download directory: %v", err)
		}

		opts := apigee.DownloadOptions{
			Host:      host,
			Org:       org,
			Proxy:     proxy,
			Token:     token,
			Revision:  *revision,
			OutputDir: downloadPath,
		}

		if err := apigee.DownloadProxyEndpoints(opts); err != nil {
			log.Fatalf("download ProxyEndpoint XML: %v", err)
		}

		matchPath, score, err := apigee.FindClosestProxyEndpoint(*outputPath, downloadPath)
		if err != nil {
			log.Printf("warning: unable to find closest ProxyEndpoint: %v", err)
			return
		}

		fmt.Printf("Closest downloaded ProxyEndpoint: %s (%.1f%% similarity)\n", matchPath, score*100)

		genFlows, err := proxyxml.ParseFlowsFromFile(*outputPath)
		if err != nil {
			log.Printf("warning: parse generated ProxyEndpoint flows: %v", err)
			return
		}
		existingFlows, err := proxyxml.ParseFlowsFromFile(matchPath)
		if err != nil {
			log.Printf("warning: parse downloaded ProxyEndpoint flows: %v", err)
			return
		}
		diff := proxyxml.DiffFlows(genFlows, existingFlows)
		if !report.PrintFlowDiff(diff) {
			return
		}

		ok, err := update.ConfirmApply()
		if err != nil {
			log.Printf("warning: confirm apply failed: %v", err)
			return
		}
		if ok {
			if err := update.ReplaceProxyEndpoint(*outputPath, matchPath); err != nil {
				log.Printf("warning: failed to update %s: %v", matchPath, err)
			} else {
				fmt.Printf("Updated %s with generated ProxyEndpoint content.\n", matchPath)
			}
		} else {
			fmt.Println("Skipped updating downloaded ProxyEndpoint.")
		}
	}
}

func resolveApigeeConfig(flagOrg, flagToken, flagHost string) (org, token, host string) {
	org = strings.TrimSpace(flagOrg)
	if org == "" {
		org = strings.TrimSpace(os.Getenv("APIGEE_ORG"))
	}

	token = strings.TrimSpace(flagToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("APIGEE_TOKEN"))
	}

	host = strings.TrimSpace(flagHost)
	if host == "" {
		host = "https://apigee.googleapis.com"
	}
	return
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
