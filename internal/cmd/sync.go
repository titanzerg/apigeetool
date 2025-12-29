package cmd

import (
	"context"
	"fmt"
	"strings"
)

const (
	syncTargetRequiredErr = "sync requires at least one target: apiproxy, target_server, api_product, or all"
	noneLabel             = "<none>"
	deleteFromPrefix      = "DELETE FROM "
	clearTableFmt         = "clear %s: %w"
)

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
		return SyncSelection{}, fmt.Errorf(syncTargetRequiredErr)
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
		return SyncSelection{}, fmt.Errorf(syncTargetRequiredErr)
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
		return fmt.Errorf(syncTargetRequiredErr)
	}

	dbURL, err := resolveSyncDBURL(args)
	if err != nil {
		return err
	}

	table, targetTable, productsTable := syncTableNames(args)
	progress := syncProxyScanProgressPrinter()

	dbOpts := syncDBOptions(args, dbURL)

	ctx := context.Background()
	pool, err := openDBPool(ctx, dbOpts)
	if err != nil {
		return err
	}
	defer pool.Close()

	endpoints, err := collectProxyEndpointsIfNeeded(cfg, selection, progress)
	if err != nil {
		return err
	}
	if err := syncProxyEndpointsIfRequested(ctx, pool, selection, table, endpoints); err != nil {
		return err
	}
	if err := syncTargetServersIfRequested(ctx, pool, selection, targetTable, cfg, endpoints); err != nil {
		return err
	}
	if err := syncAPIProductsIfRequested(ctx, pool, selection, productsTable, cfg); err != nil {
		return err
	}

	return nil
}
