package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"apigee/internal/cmd"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if err := loadDotEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
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

	cfg := cmd.ResolveApigeeConfig(*apigeeOrg, *apigeeToken, *apigeeHost)

	if targetBase := strings.TrimSpace(*findBase); targetBase != "" {
		return cmd.RunFindProxy(cfg, targetBase)
	}

	if *syncFlag {
		return cmd.RunSync(cfg, cmd.SyncArgs{
			DBURL:          strings.TrimSpace(*syncDBURL),
			EndpointsTable: strings.TrimSpace(*syncEndpointsTable),
			TargetTable:    strings.TrimSpace(*syncTargetTable),
			ProductsTable:  strings.TrimSpace(*syncProductsTable),
			SSLRoot:        strings.TrimSpace(*syncSSLRoot),
			SSLCert:        strings.TrimSpace(*syncSSLCert),
			SSLKey:         strings.TrimSpace(*syncSSLKey),
		})
	}

	return cmd.RunGenerate(cfg, cmd.GenerateArgs{
		InputPath:   strings.TrimSpace(*inputPath),
		OutputPath:  strings.TrimSpace(*outputPath),
		Name:        strings.TrimSpace(*name),
		BasePath:    strings.TrimSpace(*basePath),
		ProxyName:   strings.TrimSpace(*proxyName),
		Revision:    *revision,
		DownloadDir: strings.TrimSpace(*downloadDir),
	})
}
