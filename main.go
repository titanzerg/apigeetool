package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"

	"apigee/internal/cmd"
)

type syncFlagValue struct {
	raw string
}

func (s *syncFlagValue) String() string {
	return s.raw
}

func (s *syncFlagValue) Set(val string) error {
	val = strings.TrimSpace(val)
	if val == "" || strings.EqualFold(val, "true") {
		s.raw = "all"
		return nil
	}
	s.raw = val
	return nil
}

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
		apigeeToken = flag.String("token", "", "Apigee OAuth token (defaults to APIGEE_TOKEN env var)")

		dbURL     = flag.String("db-url", "", "PostgreSQL connection URL for -sync and -findproxy DB cache (defaults to APIGEE_SYNC_DB_URL or DATABASE_URL)")
		dbSSLRoot = flag.String("db-ssl-rootcert", "", "Path to CA certificate for DB connection (defaults to APIGEE_SYNC_DB_SSL_ROOTCERT)")
		dbSSLCert = flag.String("db-ssl-cert", "", "Path to client certificate for DB connection (defaults to APIGEE_SYNC_DB_SSL_CERT)")
		dbSSLKey  = flag.String("db-ssl-key", "", "Path to client key for DB connection (defaults to APIGEE_SYNC_DB_SSL_KEY)")

		findBase = flag.String("findproxy", "", "Find Apigee proxies that use the specified BasePath")

		syncFlag       syncFlagValue
		endpointsTable = flag.String("endpoints-table", "apigee.apigee_proxy_endpoints", "PostgreSQL table for proxy endpoints (used by -sync and -findproxy cache lookups)")
		targetsTable   = flag.String("targets-table", "apigee.apigee_target_servers", "PostgreSQL table for target servers (sync mode)")
		productsTable  = flag.String("products-table", "apigee.apigee_api_products", "PostgreSQL table for API products (sync mode)")
		compareMode    = flag.Bool("compare", false, "Compare two Apigee proxy revisions (requires -proxy and two revision numbers)")
	)

	flag.StringVar(proxyName, "p", "", "alias for -proxy")
	flag.StringVar(findBase, "f", "", "alias for -findproxy")
	flag.BoolVar(compareMode, "c", false, "alias for -compare")
	flag.Var(&syncFlag, "sync", "Sync Apigee data into PostgreSQL (all|apiproxy|target_server|api_product)")
	flag.Parse()

	cfg := cmd.ResolveApigeeConfig(*apigeeOrg, *apigeeToken, *apigeeHost)

	if targetBase := strings.TrimSpace(*findBase); targetBase != "" {
		return cmd.RunFindProxy(cfg, cmd.FindArgs{
			BasePath: targetBase,
			DBURL:    strings.TrimSpace(*dbURL),
			Table:    strings.TrimSpace(*endpointsTable),
			SSLRoot:  strings.TrimSpace(*dbSSLRoot),
			SSLCert:  strings.TrimSpace(*dbSSLCert),
			SSLKey:   strings.TrimSpace(*dbSSLKey),
		})
	}

	if syncValue := strings.TrimSpace(syncFlag.raw); syncValue != "" {
		selection, err := cmd.ParseSyncSelection(syncValue)
		if err != nil {
			return err
		}
		return cmd.RunSync(cfg, cmd.SyncArgs{
			Selection:      selection,
			DBURL:          strings.TrimSpace(*dbURL),
			EndpointsTable: strings.TrimSpace(*endpointsTable),
			TargetTable:    strings.TrimSpace(*targetsTable),
			ProductsTable:  strings.TrimSpace(*productsTable),
			SSLRoot:        strings.TrimSpace(*dbSSLRoot),
			SSLCert:        strings.TrimSpace(*dbSSLCert),
			SSLKey:         strings.TrimSpace(*dbSSLKey),
		})
	}

	if *compareMode {
		args := flag.Args()
		if len(args) != 2 {
			return fmt.Errorf("compare requires exactly two revision numbers (e.g. -c 2 1)")
		}
		revA, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid revision %q", args[0])
		}
		revB, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid revision %q", args[1])
		}
		return cmd.RunCompare(cfg, cmd.CompareArgs{
			ProxyName:   strings.TrimSpace(*proxyName),
			RevisionA:   revA,
			RevisionB:   revB,
			DownloadDir: strings.TrimSpace(*downloadDir),
		})
	}

	deploy := strings.TrimSpace(*proxyName) != ""
	return cmd.RunGenerate(cfg, cmd.GenerateArgs{
		InputPath:   strings.TrimSpace(*inputPath),
		OutputPath:  strings.TrimSpace(*outputPath),
		Name:        strings.TrimSpace(*name),
		BasePath:    strings.TrimSpace(*basePath),
		ProxyName:   strings.TrimSpace(*proxyName),
		Revision:    *revision,
		DownloadDir: strings.TrimSpace(*downloadDir),
		Deploy:      deploy,
	})
}
