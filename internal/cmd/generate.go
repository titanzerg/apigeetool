package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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
		if args.Deploy {
			if err := deployExistingRevision(cfg, proxy, args.Revision); err != nil {
				log.Printf("warning: deploy latest proxy failed: %v", err)
			}
		}
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
			if args.Deploy {
				if err := deployUpdatedProxy(cfg, proxy, args.Revision, matchPath); err != nil {
					log.Printf("warning: deploy updated proxy failed: %v", err)
				}
			}
		}
	} else {
		fmt.Println("Skipped updating downloaded ProxyEndpoint.")
	}

	return nil
}

func deployExistingRevision(cfg ApigeeConfig, proxy string, revision int) error {
	if err := RequireApigeeAuth(cfg, "deploying proxies"); err != nil {
		return err
	}
	client := apigee.NewClient(cfg.Host, cfg.Org, cfg.Token)
	baseRev := revision
	if baseRev <= 0 {
		latest, err := client.LatestRevision(proxy)
		if err != nil {
			return fmt.Errorf("resolve latest revision: %w", err)
		}
		baseRev = latest
	}

	deployedByEnv, err := client.DeployedRevisions(proxy)
	if err != nil {
		return fmt.Errorf("list deployed revisions: %w", err)
	}
	if len(deployedByEnv) == 0 {
		fmt.Println("No deployed environments found; skipping deploy.")
		return nil
	}

	envs := filterOlderEnvs(deployedByEnv, baseRev)
	if len(envs) == 0 {
		fmt.Printf("No environments with older revisions than %d; skipping deploy.\n", baseRev)
		return nil
	}
	sort.Strings(envs)

	selected, err := promptEnvironments(envs, deployedByEnv)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Println("No environments selected; skipping deploy.")
		return nil
	}

	for _, env := range selected {
		if err := client.DeployRevision(proxy, env, baseRev, true); err != nil {
			return fmt.Errorf("deploy revision %d to %s: %w", baseRev, env, err)
		}
		fmt.Printf("Deployed revision %d to %s.\n", baseRev, env)
	}
	return nil
}

func deployUpdatedProxy(cfg ApigeeConfig, proxy string, revision int, updatedPath string) error {
	if err := RequireApigeeAuth(cfg, "deploying proxies"); err != nil {
		return err
	}
	client := apigee.NewClient(cfg.Host, cfg.Org, cfg.Token)
	baseRev := revision
	if baseRev <= 0 {
		latest, err := client.LatestRevision(proxy)
		if err != nil {
			return fmt.Errorf("resolve latest revision: %w", err)
		}
		baseRev = latest
	}

	envs, err := client.EnvironmentsForRevision(proxy, baseRev)
	if err != nil {
		return fmt.Errorf("list environments for revision %d: %w", baseRev, err)
	}
	if len(envs) == 0 {
		fmt.Printf("No deployed environments found for revision %d; skipping deploy.\n", baseRev)
		return nil
	}

	deployedByEnv, err := client.DeployedRevisions(proxy)
	if err != nil {
		return fmt.Errorf("list deployed revisions: %w", err)
	}

	envs = filterOlderEnvsByList(deployedByEnv, envs, baseRev)
	if len(envs) == 0 {
		fmt.Printf("No environments with older revisions than %d; skipping deploy.\n", baseRev)
		return nil
	}

	selected, err := promptEnvironments(envs, deployedByEnv)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Println("No environments selected; skipping deploy.")
		return nil
	}

	bundle, err := client.FetchProxyBundle(proxy, baseRev)
	if err != nil {
		return fmt.Errorf("fetch proxy bundle: %w", err)
	}
	content, err := os.ReadFile(updatedPath)
	if err != nil {
		return fmt.Errorf("read updated ProxyEndpoint: %w", err)
	}

	updatedBundle, updatedEntry, err := apigee.ReplaceProxyEndpointInBundle(bundle, filepath.Base(updatedPath), content)
	if err != nil {
		return fmt.Errorf("update proxy bundle: %w", err)
	}

	newRev, err := client.ImportProxyBundle(proxy, updatedBundle)
	if err != nil {
		return fmt.Errorf("import proxy bundle: %w", err)
	}
	fmt.Printf("Imported new revision %d (updated %s from revision %d).\n", newRev, updatedEntry, baseRev)

	for _, env := range selected {
		if current, ok := deployedByEnv[env]; ok && current >= newRev {
			fmt.Printf("Skipped deploy to %s (current revision %d >= new %d).\n", env, current, newRev)
			continue
		}
		if err := client.DeployRevision(proxy, env, newRev, true); err != nil {
			return fmt.Errorf("deploy revision %d to %s: %w", newRev, env, err)
		}
		fmt.Printf("Deployed revision %d to %s.\n", newRev, env)
	}
	return nil
}

func filterOlderEnvs(currentByEnv map[string]int, target int) []string {
	var envs []string
	for env, current := range currentByEnv {
		if current > 0 && current < target {
			envs = append(envs, env)
		}
	}
	return envs
}

func filterOlderEnvsByList(currentByEnv map[string]int, envs []string, target int) []string {
	var filtered []string
	for _, env := range envs {
		if current, ok := currentByEnv[env]; ok && current > 0 && current < target {
			filtered = append(filtered, env)
		}
	}
	return filtered
}

func promptEnvironments(envs []string, currentByEnv map[string]int) ([]string, error) {
	reader := bufio.NewReader(os.Stdin)
	var selected []string
	for _, env := range envs {
		for {
			if rev, ok := currentByEnv[env]; ok && rev > 0 {
				fmt.Printf("Deploy to environment %s (current revision %d)? [y/N]: ", env, rev)
			} else {
				fmt.Printf("Deploy to environment %s (current revision unknown)? [y/N]: ", env)
			}
			resp, err := reader.ReadString('\n')
			if err != nil {
				return nil, err
			}
			resp = strings.TrimSpace(strings.ToLower(resp))
			if resp == "" || resp == "n" || resp == "no" {
				break
			}
			if resp == "y" || resp == "yes" {
				selected = append(selected, env)
				break
			}
			fmt.Println("Please answer y or n.")
		}
	}
	return selected, nil
}
