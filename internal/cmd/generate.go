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
	output, flowCount, err := generateProxyEndpoint(args)
	if err != nil {
		return err
	}
	fmt.Printf("Generated %d flows at %s\n", flowCount, output)

	proxy := strings.TrimSpace(args.ProxyName)
	if proxy == "" {
		return nil
	}

	if err := RequireApigeeAuth(cfg, "downloading proxies"); err != nil {
		return err
	}
	downloadPath, err := prepareDownloadDir(args.DownloadDir)
	if err != nil {
		return err
	}
	if err := downloadProxyEndpoints(cfg, proxy, args.Revision, downloadPath); err != nil {
		return err
	}
	matchPath, score, err := apigee.FindClosestProxyEndpoint(output, downloadPath)
	if err != nil {
		log.Printf("warning: unable to find closest ProxyEndpoint: %v", err)
		return nil
	}

	fmt.Printf("Closest downloaded ProxyEndpoint: %s (%.1f%% similarity)\n", matchPath, score*100)

	diff, err := compareProxyEndpoints(output, matchPath)
	if err != nil {
		log.Printf("warning: %v", err)
		return nil
	}
	if !report.PrintFlowDiff(diff) {
		if args.Deploy {
			if err := deployExistingRevision(cfg, proxy, args.Revision); err != nil {
				log.Printf("warning: deploy latest proxy failed: %v", err)
			}
		}
		return nil
	}

	applyProxyEndpointUpdate(cfg, args, proxy, output, matchPath)
	return nil
}

func generateProxyEndpoint(args GenerateArgs) (string, int, error) {
	data, err := os.ReadFile(DefaultString(args.InputPath, "openapi.yaml"))
	if err != nil {
		return "", 0, fmt.Errorf("read OpenAPI document: %w", err)
	}

	spec, err := openapi.Parse(data)
	if err != nil {
		return "", 0, fmt.Errorf("parse OpenAPI YAML: %w", err)
	}

	if len(spec.Paths) == 0 {
		return "", 0, fmt.Errorf("no paths defined in OpenAPI document")
	}

	ordering := openapi.ExtractPathOrdering(data)
	flows := openapi.BuildFlows(spec.Paths, ordering)
	if len(flows) == 0 {
		return "", 0, fmt.Errorf("no operations found under OpenAPI paths")
	}

	path := strings.TrimSpace(args.BasePath)
	if path == "" {
		path = openapi.SlugifyTitle(spec.Info.Title)
	}

	output := DefaultString(args.OutputPath, "proxy-endpoint.xml")
	xml := openapi.RenderProxyEndpoint(DefaultString(args.Name, "default"), path, flows)

	if err := os.WriteFile(output, []byte(xml), 0o644); err != nil {
		return "", 0, fmt.Errorf("write output XML: %w", err)
	}

	return output, len(flows), nil
}

func prepareDownloadDir(dir string) (string, error) {
	downloadPath := DefaultString(dir, "downloaded-proxy-endpoints")
	if err := os.RemoveAll(downloadPath); err != nil {
		return "", fmt.Errorf("cleanup download directory: %w", err)
	}
	if err := os.MkdirAll(downloadPath, 0o755); err != nil {
		return "", fmt.Errorf("create download directory: %w", err)
	}
	return downloadPath, nil
}

func downloadProxyEndpoints(cfg ApigeeConfig, proxy string, revision int, downloadPath string) error {
	opts := apigee.DownloadOptions{
		Host:      cfg.Host,
		Org:       cfg.Org,
		Proxy:     proxy,
		Token:     cfg.Token,
		Revision:  revision,
		OutputDir: downloadPath,
	}
	if err := apigee.DownloadProxyEndpoints(opts); err != nil {
		return fmt.Errorf("download ProxyEndpoint XML: %w", err)
	}
	return nil
}

func compareProxyEndpoints(generatedPath, existingPath string) (proxyxml.FlowDiff, error) {
	genFlows, err := proxyxml.ParseFlowsFromFile(generatedPath)
	if err != nil {
		return proxyxml.FlowDiff{}, fmt.Errorf("parse generated ProxyEndpoint flows: %w", err)
	}
	existingFlows, err := proxyxml.ParseFlowsFromFile(existingPath)
	if err != nil {
		return proxyxml.FlowDiff{}, fmt.Errorf("parse downloaded ProxyEndpoint flows: %w", err)
	}
	return proxyxml.DiffFlows(genFlows, existingFlows), nil
}

func applyProxyEndpointUpdate(cfg ApigeeConfig, args GenerateArgs, proxy, output, matchPath string) {
	ok, err := update.ConfirmApply()
	if err != nil {
		log.Printf("warning: confirm apply failed: %v", err)
		return
	}
	if !ok {
		fmt.Println("Skipped updating downloaded ProxyEndpoint.")
		return
	}
	if err := update.ReplaceProxyEndpoint(output, matchPath); err != nil {
		log.Printf("warning: failed to update %s: %v", matchPath, err)
		return
	}
	fmt.Printf("Updated %s with generated ProxyEndpoint content.\n", matchPath)
	if args.Deploy {
		if err := deployUpdatedProxy(cfg, proxy, args.Revision, matchPath); err != nil {
			log.Printf("warning: deploy updated proxy failed: %v", err)
		}
	}
}

func deployExistingRevision(cfg ApigeeConfig, proxy string, revision int) error {
	client, baseRev, err := buildDeployContext(cfg, proxy, revision)
	if err != nil {
		return err
	}

	deployedByEnv, err := client.DeployedRevisions(proxy)
	if err != nil {
		return fmt.Errorf("list deployed revisions: %w", err)
	}
	if len(deployedByEnv) == 0 {
		fmt.Println("No deployed environments found; skipping deploy.")
		return nil
	}

	envs := envsFromMap(deployedByEnv)
	selected, err := selectDeployEnvs(envs, deployedByEnv, baseRev)
	if err != nil || len(selected) == 0 {
		return err
	}

	return deployRevisionToEnvs(client, proxy, baseRev, selected)
}

func deployUpdatedProxy(cfg ApigeeConfig, proxy string, revision int, updatedPath string) error {
	client, baseRev, err := buildDeployContext(cfg, proxy, revision)
	if err != nil {
		return err
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

	selected, err := selectDeployEnvs(envs, deployedByEnv, baseRev)
	if err != nil || len(selected) == 0 {
		return err
	}

	newRev, err := importUpdatedRevision(client, proxy, baseRev, updatedPath)
	if err != nil {
		return err
	}

	return deployRevisionToEnvsIfNewer(client, proxy, newRev, selected, deployedByEnv)
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

func buildDeployContext(cfg ApigeeConfig, proxy string, revision int) (*apigee.Client, int, error) {
	if err := RequireApigeeAuth(cfg, "deploying proxies"); err != nil {
		return nil, 0, err
	}
	client := apigee.NewClient(cfg.Host, cfg.Org, cfg.Token)
	baseRev := revision
	if baseRev <= 0 {
		latest, err := client.LatestRevision(proxy)
		if err != nil {
			return nil, 0, fmt.Errorf("resolve latest revision: %w", err)
		}
		baseRev = latest
	}
	return client, baseRev, nil
}

func envsFromMap(currentByEnv map[string]int) []string {
	envs := make([]string, 0, len(currentByEnv))
	for env := range currentByEnv {
		envs = append(envs, env)
	}
	return envs
}

func selectDeployEnvs(envs []string, currentByEnv map[string]int, target int) ([]string, error) {
	envs = filterOlderEnvsByList(currentByEnv, envs, target)
	if len(envs) == 0 {
		fmt.Printf("No environments with older revisions than %d; skipping deploy.\n", target)
		return nil, nil
	}
	sort.Strings(envs)

	selected, err := promptEnvironments(envs, currentByEnv)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		fmt.Println("No environments selected; skipping deploy.")
		return nil, nil
	}
	return selected, nil
}

func importUpdatedRevision(client *apigee.Client, proxy string, baseRev int, updatedPath string) (int, error) {
	bundle, err := client.FetchProxyBundle(proxy, baseRev)
	if err != nil {
		return 0, fmt.Errorf("fetch proxy bundle: %w", err)
	}
	content, err := os.ReadFile(updatedPath)
	if err != nil {
		return 0, fmt.Errorf("read updated ProxyEndpoint: %w", err)
	}

	updatedBundle, updatedEntry, err := apigee.ReplaceProxyEndpointInBundle(bundle, filepath.Base(updatedPath), content)
	if err != nil {
		return 0, fmt.Errorf("update proxy bundle: %w", err)
	}

	newRev, err := client.ImportProxyBundle(proxy, updatedBundle)
	if err != nil {
		return 0, fmt.Errorf("import proxy bundle: %w", err)
	}
	fmt.Printf("Imported new revision %d (updated %s from revision %d).\n", newRev, updatedEntry, baseRev)
	return newRev, nil
}

func deployRevisionToEnvs(client *apigee.Client, proxy string, revision int, envs []string) error {
	for _, env := range envs {
		if err := client.DeployRevision(proxy, env, revision, true); err != nil {
			return fmt.Errorf("deploy revision %d to %s: %w", revision, env, err)
		}
		fmt.Printf("Deployed revision %d to %s.\n", revision, env)
	}
	return nil
}

func deployRevisionToEnvsIfNewer(client *apigee.Client, proxy string, revision int, envs []string, currentByEnv map[string]int) error {
	for _, env := range envs {
		if current, ok := currentByEnv[env]; ok && current >= revision {
			fmt.Printf("Skipped deploy to %s (current revision %d >= new %d).\n", env, current, revision)
			continue
		}
		if err := client.DeployRevision(proxy, env, revision, true); err != nil {
			return fmt.Errorf("deploy revision %d to %s: %w", revision, env, err)
		}
		fmt.Printf("Deployed revision %d to %s.\n", revision, env)
	}
	return nil
}

func promptEnvironments(envs []string, currentByEnv map[string]int) ([]string, error) {
	reader := bufio.NewReader(os.Stdin)
	var selected []string
	for _, env := range envs {
		for {
			fmt.Print(buildDeployPrompt(env, currentByEnv))
			resp, err := readPromptResponse(reader)
			if err != nil {
				return nil, err
			}
			action := classifyPromptResponse(resp)
			if action == promptNo {
				break
			}
			if action == promptYes {
				selected = append(selected, env)
				break
			}
			fmt.Println("Please answer y or n.")
		}
	}
	return selected, nil
}

type promptAction int

const (
	promptInvalid promptAction = iota
	promptNo
	promptYes
)

func buildDeployPrompt(env string, currentByEnv map[string]int) string {
	if rev, ok := currentByEnv[env]; ok && rev > 0 {
		return fmt.Sprintf("Deploy to environment %s (current revision %d)? [y/N]: ", env, rev)
	}
	return fmt.Sprintf("Deploy to environment %s (current revision unknown)? [y/N]: ", env)
}

func readPromptResponse(reader *bufio.Reader) (string, error) {
	resp, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.ToLower(resp)), nil
}

func classifyPromptResponse(resp string) promptAction {
	if resp == "" || resp == "n" || resp == "no" {
		return promptNo
	}
	if resp == "y" || resp == "yes" {
		return promptYes
	}
	return promptInvalid
}
