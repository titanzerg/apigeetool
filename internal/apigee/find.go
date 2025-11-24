package apigee

import (
	"fmt"
	"strings"
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
}

// ProxyEndpointRecord describes a ProxyEndpoint retrieved from Apigee.
type ProxyEndpointRecord struct {
	Proxy    string
	Endpoint string
	Revision int
	BasePath string
	Targets  []string
	Envs     []string
	Flows    int
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
	base := normalizeBasePath(strings.TrimSpace(opts.BasePath))
	if base == "" {
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
			if opts.Progress != nil {
				opts.Progress(ProxyScanProgress{
					Index:    i + 1,
					Total:    total,
					Proxy:    proxy,
					Revision: rev,
					Err:      fmt.Errorf("latest revision: %w", err),
				})
			}
			continue
		}
		if rev == 0 {
			continue
		}

		bundle, err := client.FetchProxyBundle(proxy, rev)
		if err != nil {
			if opts.Progress != nil {
				opts.Progress(ProxyScanProgress{
					Index:    i + 1,
					Total:    total,
					Proxy:    proxy,
					Revision: rev,
					Err:      fmt.Errorf("fetch proxy bundle: %w", err),
				})
			}
			continue
		}
		endpoints, err := parseProxyEndpointsFromBundle(bundle)
		if err != nil {
			if opts.Progress != nil {
				opts.Progress(ProxyScanProgress{
					Index:    i + 1,
					Total:    total,
					Proxy:    proxy,
					Revision: rev,
					Err:      fmt.Errorf("parse proxy endpoints: %w", err),
				})
			}
			continue
		}
		var matched bool
		for _, endpoint := range endpoints {
			if normalizeBasePath(endpoint.BasePath) == base {
				matched = true
				matches = append(matches, ProxyMatch{
					Proxy:    proxy,
					Endpoint: endpoint.Name,
					Revision: rev,
					BasePath: endpoint.BasePath,
				})
			}
		}
		if opts.Progress != nil {
			opts.Progress(ProxyScanProgress{
				Index:     i + 1,
				Total:     total,
				Proxy:     proxy,
				Revision:  rev,
				BasePaths: uniqueBasePaths(endpoints),
				Matched:   matched,
			})
		}
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
			if opts.Progress != nil {
				opts.Progress(ProxyScanProgress{
					Index:    i + 1,
					Total:    total,
					Proxy:    proxy,
					Err:      fmt.Errorf("latest revision: %w", err),
					Revision: rev,
				})
			}
			continue
		}
		if rev == 0 {
			continue
		}
		bundle, err := client.FetchProxyBundle(proxy, rev)
		if err != nil {
			if opts.Progress != nil {
				opts.Progress(ProxyScanProgress{
					Index:    i + 1,
					Total:    total,
					Proxy:    proxy,
					Revision: rev,
					Err:      fmt.Errorf("fetch proxy bundle: %w", err),
				})
			}
			continue
		}
		endpoints, err := parseProxyEndpointsFromBundle(bundle)
		if err != nil {
			if opts.Progress != nil {
				opts.Progress(ProxyScanProgress{
					Index:    i + 1,
					Total:    total,
					Proxy:    proxy,
					Revision: rev,
					Err:      fmt.Errorf("parse proxy endpoints: %w", err),
				})
			}
			continue
		}
		envs, envErr := client.EnvironmentsForRevision(proxy, rev)
		if len(envs) == 0 {
			if allEnvs, listErr := client.ListEnvironments(); listErr == nil {
				envs = allEnvs
			} else if envErr == nil {
				envErr = fmt.Errorf("list environments: %w", listErr)
			}
		}
		for _, endpoint := range endpoints {
			endpointsOut = append(endpointsOut, ProxyEndpointRecord{
				Proxy:    proxy,
				Endpoint: endpoint.Name,
				Revision: rev,
				BasePath: endpoint.BasePath,
				Targets:  endpoint.TargetServers,
				Envs:     envs,
				Flows:    endpoint.FlowCount,
			})
		}
		if opts.Progress != nil {
			opts.Progress(ProxyScanProgress{
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
		base := strings.TrimSpace(ep.BasePath)
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

// CollectTargetServers fetches target server details for environments referenced by the endpoints.
func CollectTargetServers(opts CollectTargetServersOptions) ([]TargetServerRecord, error) {
	client, err := resolveClient(opts.Client, opts.Host, opts.Org, opts.Token)
	if err != nil {
		return nil, err
	}

	envSet := make(map[string]struct{})
	for _, ep := range opts.Endpoints {
		for _, env := range ep.Envs {
			env = strings.TrimSpace(env)
			if env == "" {
				continue
			}
			envSet[env] = struct{}{}
		}
	}

	var records []TargetServerRecord
	for env := range envSet {
		targetNames, err := client.ListTargetServers(env)
		if err != nil {
			return nil, fmt.Errorf("list target servers in %s: %w", env, err)
		}
		for _, tgt := range targetNames {
			rec, err := client.FetchTargetServer(env, tgt)
			if err != nil {
				return nil, fmt.Errorf("fetch target server %s/%s: %w", env, tgt, err)
			}
			records = append(records, rec)
		}
	}
	return records, nil
}
