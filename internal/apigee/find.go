package apigee

import (
	"fmt"
	"strings"

	"apigee/internal/proxyxml"
	"apigee/internal/util"
)

// FindProxyOptions contains search parameters for locating proxies by BasePath.
type FindProxyOptions struct {
	Host     string
	Org      string
	Token    string
	BasePath string
	Progress func(ProxyScanProgress)
	Client   ManagementClient
}

// ProxyMatch describes a proxy whose BasePath matched the requested value.
type ProxyMatch struct {
	Proxy    string
	Endpoint string
	Revision int
	BasePath string
}

// ProxyScanProgress contains metadata about each proxy evaluation.
type ProxyScanProgress struct {
	Index     int
	Total     int
	Proxy     string
	Revision  int
	BasePaths []string
	Envs      []string
	EnvError  string
	Matched   bool
	Err       error
}

// CollectProxyEndpointsOptions controls the behavior of CollectProxyEndpoints.
type CollectProxyEndpointsOptions struct {
	Host     string
	Org      string
	Token    string
	Progress func(ProxyScanProgress)
	Client   ManagementClient
}

// CollectTargetServersOptions controls the behavior of CollectTargetServers.
type CollectTargetServersOptions struct {
	Host      string
	Org       string
	Token     string
	Endpoints []ProxyEndpointRecord
	Client    ManagementClient
	Progress  func(TargetServerProgress)
}

// TargetServerProgress describes progress while fetching target servers.
type TargetServerProgress struct {
	Index       int
	Total       int
	Environment string
	Name        string
	URL         string
	Err         error
}

// ProxyEndpointRecord describes a ProxyEndpoint retrieved from Apigee.
type ProxyEndpointRecord struct {
	Proxy     string
	Endpoint  string
	Revision  int
	BasePath  string
	Targets   []string
	Envs      []string
	Flows     int
	FlowSteps proxyxml.FlowSteps
}

// TargetServerRecord describes a target server with environment context.
type TargetServerRecord struct {
	Name        string
	Environment string
	URL         string
	Host        string
	Port        int
	IsSSL       bool
}

// FindProxiesByBasePath lists Apigee proxies with a matching BasePath.
func FindProxiesByBasePath(opts FindProxyOptions) ([]ProxyMatch, error) {
	searchTerm := strings.TrimSpace(opts.BasePath)
	if searchTerm == "" {
		return nil, fmt.Errorf("base path is required")
	}

	client, err := resolveClient(opts.Client, opts.Host, opts.Org, opts.Token)
	if err != nil {
		return nil, err
	}

	proxies, err := client.ListAPIs()
	if err != nil {
		return nil, fmt.Errorf("list apis: %w", err)
	}

	var matches []ProxyMatch
	total := len(proxies)
	for i, proxy := range proxies {
		rev, err := client.LatestRevision(proxy)
		if err != nil {
			reportProxyProgress(opts.Progress, ProxyScanProgress{
				Index:    i + 1,
				Total:    total,
				Proxy:    proxy,
				Revision: rev,
				Err:      fmt.Errorf("latest revision: %w", err),
			})
			continue
		}
		if rev == 0 {
			continue
		}

		endpoints, err := fetchProxyEndpoints(client, proxy, rev)
		if err != nil {
			reportProxyProgress(opts.Progress, ProxyScanProgress{
				Index:    i + 1,
				Total:    total,
				Proxy:    proxy,
				Revision: rev,
				Err:      err,
			})
			continue
		}

		var matched bool
		matches, matched = appendMatchingProxies(matches, proxy, rev, endpoints, searchTerm)
		reportProxyProgress(opts.Progress, ProxyScanProgress{
			Index:     i + 1,
			Total:     total,
			Proxy:     proxy,
			Revision:  rev,
			BasePaths: uniqueBasePaths(endpoints),
			Matched:   matched,
		})
	}

	return matches, nil
}

// CollectProxyEndpoints fetches the latest revision for every proxy and returns all ProxyEndpoint definitions.
func CollectProxyEndpoints(opts CollectProxyEndpointsOptions) ([]ProxyEndpointRecord, error) {
	client, err := resolveClient(opts.Client, opts.Host, opts.Org, opts.Token)
	if err != nil {
		return nil, err
	}

	proxies, err := client.ListAPIs()
	if err != nil {
		return nil, fmt.Errorf("list apis: %w", err)
	}

	total := len(proxies)
	var endpointsOut []ProxyEndpointRecord
	for i, proxy := range proxies {
		rev, err := client.LatestRevision(proxy)
		if err != nil {
			reportProxyProgress(opts.Progress, ProxyScanProgress{
				Index:    i + 1,
				Total:    total,
				Proxy:    proxy,
				Err:      fmt.Errorf("latest revision: %w", err),
				Revision: rev,
			})
			continue
		}
		if rev == 0 {
			continue
		}
		endpoints, err := fetchProxyEndpoints(client, proxy, rev)
		if err != nil {
			reportProxyProgress(opts.Progress, ProxyScanProgress{
				Index:    i + 1,
				Total:    total,
				Proxy:    proxy,
				Revision: rev,
				Err:      err,
			})
			continue
		}
		envs, envErr := resolveProxyEnvironments(client, proxy, rev)
		endpointsOut = appendProxyEndpoints(endpointsOut, proxy, rev, envs, endpoints)
		reportProxyProgress(opts.Progress, ProxyScanProgress{
			Index:     i + 1,
			Total:     total,
			Proxy:     proxy,
			Revision:  rev,
			BasePaths: uniqueBasePaths(endpoints),
			Envs:      envs,
			EnvError:  errString(envErr),
			Matched:   len(endpoints) > 0,
		})
	}

	return endpointsOut, nil
}

func normalizeBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func uniqueBasePaths(endpoints []bundleEndpoint) []string {
	seen := make(map[string]struct{}, len(endpoints))
	var result []string
	for _, ep := range endpoints {
		base := normalizeBasePath(ep.BasePath)
		if _, ok := seen[base]; ok {
			continue
		}
		seen[base] = struct{}{}
		result = append(result, base)
	}
	return result
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func basePathContains(target, query string) bool {
	t := normalizeBasePathForSearch(target)
	q := normalizeBasePathForSearch(query)
	if t == "" || q == "" {
		return false
	}
	return strings.Contains(t, q)
}

func normalizeBasePathForSearch(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	return strings.ToLower(path)
}

// CollectTargetServers fetches target server details for environments referenced by the endpoints.
func CollectTargetServers(opts CollectTargetServersOptions) ([]TargetServerRecord, error) {
	client, err := resolveClient(opts.Client, opts.Host, opts.Org, opts.Token)
	if err != nil {
		return nil, err
	}

	envs := collectTargetServerEnvs(opts.Endpoints)
	envTargets, total, err := listTargetServersByEnv(client, envs, opts.Progress)
	if err != nil {
		return nil, err
	}
	return fetchTargetServerRecords(client, envs, envTargets, total, opts.Progress)
}

func reportProxyProgress(cb func(ProxyScanProgress), progress ProxyScanProgress) {
	if cb == nil {
		return
	}
	cb(progress)
}

func fetchProxyEndpoints(client ManagementClient, proxy string, rev int) ([]bundleEndpoint, error) {
	bundle, err := client.FetchProxyBundle(proxy, rev)
	if err != nil {
		return nil, fmt.Errorf("fetch proxy bundle: %w", err)
	}
	endpoints, err := parseProxyEndpointsFromBundle(bundle)
	if err != nil {
		return nil, fmt.Errorf("parse proxy endpoints: %w", err)
	}
	return endpoints, nil
}

func appendMatchingProxies(matches []ProxyMatch, proxy string, rev int, endpoints []bundleEndpoint, searchTerm string) ([]ProxyMatch, bool) {
	var matched bool
	for _, endpoint := range endpoints {
		if basePathContains(endpoint.BasePath, searchTerm) {
			matched = true
			matches = append(matches, ProxyMatch{
				Proxy:    proxy,
				Endpoint: endpoint.Name,
				Revision: rev,
				BasePath: endpoint.BasePath,
			})
		}
	}
	return matches, matched
}

func resolveProxyEnvironments(client ManagementClient, proxy string, rev int) ([]string, error) {
	envs, envErr := client.EnvironmentsForRevision(proxy, rev)
	if len(envs) == 0 {
		if allEnvs, listErr := client.ListEnvironments(); listErr == nil {
			envs = allEnvs
		} else if envErr == nil {
			envErr = fmt.Errorf("list environments: %w", listErr)
		}
	}
	return envs, envErr
}

func appendProxyEndpoints(records []ProxyEndpointRecord, proxy string, rev int, envs []string, endpoints []bundleEndpoint) []ProxyEndpointRecord {
	for _, endpoint := range endpoints {
		records = append(records, ProxyEndpointRecord{
			Proxy:     proxy,
			Endpoint:  endpoint.Name,
			Revision:  rev,
			BasePath:  endpoint.BasePath,
			Targets:   endpoint.TargetServers,
			Envs:      envs,
			Flows:     endpoint.FlowCount,
			FlowSteps: endpoint.FlowSteps,
		})
	}
	return records
}

func collectTargetServerEnvs(endpoints []ProxyEndpointRecord) []string {
	envSet := make(map[string]struct{})
	for _, ep := range endpoints {
		for _, env := range ep.Envs {
			env = strings.TrimSpace(env)
			if env == "" {
				continue
			}
			envSet[env] = struct{}{}
		}
	}
	envs := make([]string, 0, len(envSet))
	for env := range envSet {
		envs = append(envs, env)
	}
	return util.UniqueSortedStrings(envs)
}

func listTargetServersByEnv(client ManagementClient, envs []string, progress func(TargetServerProgress)) (map[string][]string, int, error) {
	envTargets := make(map[string][]string, len(envs))
	var total int
	for _, env := range envs {
		targetNames, err := client.ListTargetServers(env)
		if err != nil {
			if progress != nil {
				progress(TargetServerProgress{
					Environment: env,
					Err:         fmt.Errorf("list target servers: %w", err),
				})
			}
			return nil, 0, fmt.Errorf("list target servers in %s: %w", env, err)
		}
		envTargets[env] = targetNames
		total += len(targetNames)
	}
	return envTargets, total, nil
}

func fetchTargetServerRecords(client ManagementClient, envs []string, envTargets map[string][]string, total int, progress func(TargetServerProgress)) ([]TargetServerRecord, error) {
	var index int
	var records []TargetServerRecord
	for _, env := range envs {
		for _, tgt := range envTargets[env] {
			index++
			rec, err := client.FetchTargetServer(env, tgt)
			if err != nil {
				if progress != nil {
					progress(TargetServerProgress{
						Index:       index,
						Total:       total,
						Environment: env,
						Name:        tgt,
						Err:         err,
					})
				}
				return nil, fmt.Errorf("fetch target server %s/%s: %w", env, tgt, err)
			}
			records = append(records, rec)
			if progress != nil {
				progress(TargetServerProgress{
					Index:       index,
					Total:       total,
					Environment: env,
					Name:        rec.Name,
					URL:         rec.URL,
				})
			}
		}
	}
	return records, nil
}
