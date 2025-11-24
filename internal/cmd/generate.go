package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"apigee/internal/apigee"
	"apigee/internal/openapi"
	"apigee/internal/proxyxml"
	"apigee/internal/report"
	"apigee/internal/update"
)

func RunGenerate(cfg ApigeeConfig, args GenerateArgs) error {
	data, err := os.ReadFile(DefaultString(args.InputPath, "openapi.yaml"))
	if err != nil {
		return fmt.Errorf("read OpenAPI document: %w", err)
	}

	spec, err := openapi.Parse(data)
	if err != nil {
		return fmt.Errorf("parse OpenAPI YAML: %w", err)
	}

	if len(spec.Paths) == 0 {
		return fmt.Errorf("no paths defined in OpenAPI document")
	}

	ordering := openapi.ExtractPathOrdering(data)
	flows := openapi.BuildFlows(spec.Paths, ordering)
	if len(flows) == 0 {
		return fmt.Errorf("no operations found under OpenAPI paths")
	}

	path := strings.TrimSpace(args.BasePath)
	if path == "" {
		path = openapi.SlugifyTitle(spec.Info.Title)
	}

	output := DefaultString(args.OutputPath, "proxy-endpoint.xml")
	xml := openapi.RenderProxyEndpoint(DefaultString(args.Name, "default"), path, flows)

	if err := os.WriteFile(output, []byte(xml), 0o644); err != nil {
		return fmt.Errorf("write output XML: %w", err)
	}

	fmt.Printf("Generated %d flows at %s\n", len(flows), output)

	proxy := strings.TrimSpace(args.ProxyName)
	if proxy == "" {
		return nil
	}

	if err := RequireApigeeAuth(cfg, "downloading proxies"); err != nil {
		return err
	}
	downloadPath := DefaultString(args.DownloadDir, "downloaded-proxy-endpoints")

	if err := os.RemoveAll(downloadPath); err != nil {
		return fmt.Errorf("cleanup download directory: %w", err)
	}
	if err := os.MkdirAll(downloadPath, 0o755); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}

	opts := apigee.DownloadOptions{
		Host:      cfg.Host,
		Org:       cfg.Org,
		Proxy:     proxy,
		Token:     cfg.Token,
		Revision:  args.Revision,
		OutputDir: downloadPath,
	}

	if err := apigee.DownloadProxyEndpoints(opts); err != nil {
		return fmt.Errorf("download ProxyEndpoint XML: %w", err)
	}

	matchPath, score, err := apigee.FindClosestProxyEndpoint(output, downloadPath)
	if err != nil {
		log.Printf("warning: unable to find closest ProxyEndpoint: %v", err)
		return nil
	}

	fmt.Printf("Closest downloaded ProxyEndpoint: %s (%.1f%% similarity)\n", matchPath, score*100)

	genFlows, err := proxyxml.ParseFlowsFromFile(output)
	if err != nil {
		log.Printf("warning: parse generated ProxyEndpoint flows: %v", err)
		return nil
	}
	existingFlows, err := proxyxml.ParseFlowsFromFile(matchPath)
	if err != nil {
		log.Printf("warning: parse downloaded ProxyEndpoint flows: %v", err)
		return nil
	}
	diff := proxyxml.DiffFlows(genFlows, existingFlows)
	if !report.PrintFlowDiff(diff) {
		return nil
	}

	ok, err := update.ConfirmApply()
	if err != nil {
		log.Printf("warning: confirm apply failed: %v", err)
		return nil
	}
	if ok {
		if err := update.ReplaceProxyEndpoint(output, matchPath); err != nil {
			log.Printf("warning: failed to update %s: %v", matchPath, err)
		} else {
			fmt.Printf("Updated %s with generated ProxyEndpoint content.\n", matchPath)
		}
	} else {
		fmt.Println("Skipped updating downloaded ProxyEndpoint.")
	}

	return nil
}
