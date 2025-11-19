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
	Matched   bool
	Err       error
}

// CollectProxyEndpointsOptions controls the behavior of CollectProxyEndpoints.
type CollectProxyEndpointsOptions struct {
	Host     string
	Org      string
	Token    string
	Progress func(ProxyScanProgress)
}

// ProxyEndpointRecord describes a ProxyEndpoint retrieved from Apigee.
type ProxyEndpointRecord struct {
	Proxy    string
	Endpoint string
	Revision int
	BasePath string
}

// FindProxiesByBasePath lists Apigee proxies with a matching BasePath.
func FindProxiesByBasePath(opts FindProxyOptions) ([]ProxyMatch, error) {
	base := normalizeBasePath(strings.TrimSpace(opts.BasePath))
	if base == "" {
		return nil, fmt.Errorf("base path is required")
	}

	org := strings.TrimSpace(opts.Org)
	token := strings.TrimSpace(opts.Token)
	host := strings.TrimSpace(opts.Host)

	if org == "" || token == "" {
		return nil, fmt.Errorf("Apigee org and token are required")
	}

	client := NewClient(host, org, token)

	proxies, err := client.listAPIs()
	if err != nil {
		return nil, fmt.Errorf("list apis: %w", err)
	}

	var matches []ProxyMatch
	total := len(proxies)
	for i, proxy := range proxies {
		rev, err := client.latestRevision(proxy)
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

		bundle, err := client.fetchProxyBundle(proxy, rev)
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
	org := strings.TrimSpace(opts.Org)
	token := strings.TrimSpace(opts.Token)
	host := strings.TrimSpace(opts.Host)

	if org == "" || token == "" {
		return nil, fmt.Errorf("Apigee org and token are required")
	}

	client := NewClient(host, org, token)
	proxies, err := client.listAPIs()
	if err != nil {
		return nil, fmt.Errorf("list apis: %w", err)
	}

	total := len(proxies)
	var endpointsOut []ProxyEndpointRecord
	for i, proxy := range proxies {
		rev, err := client.latestRevision(proxy)
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
		bundle, err := client.fetchProxyBundle(proxy, rev)
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
		for _, endpoint := range endpoints {
			endpointsOut = append(endpointsOut, ProxyEndpointRecord{
				Proxy:    proxy,
				Endpoint: endpoint.Name,
				Revision: rev,
				BasePath: endpoint.BasePath,
			})
		}
		if opts.Progress != nil {
			opts.Progress(ProxyScanProgress{
				Index:     i + 1,
				Total:     total,
				Proxy:     proxy,
				Revision:  rev,
				BasePaths: uniqueBasePaths(endpoints),
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
