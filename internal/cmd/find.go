package cmd

import (
	"fmt"
	"strings"

	"apigee/internal/apigee"
)

func RunFindProxy(cfg ApigeeConfig, basePath string) error {
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
		BasePath: basePath,
		Progress: progress,
	})
	if err != nil {
		return fmt.Errorf("find proxies by base path: %w", err)
	}
	if len(matches) == 0 {
		fmt.Printf("No Apigee proxies found with BasePath %s\n", basePath)
		return nil
	}

	fmt.Printf("Found %d Apigee proxies with BasePath %s:\n", len(matches), basePath)
	for _, match := range matches {
		fmt.Printf("- %s (endpoint %s, revision %d)\n", match.Proxy, match.Endpoint, match.Revision)
	}
	return nil
}
