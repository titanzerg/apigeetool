package proxyxml

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// ExtractRouteTargets returns TargetEndpoint names referenced in RouteRule blocks.
func ExtractRouteTargets(data []byte) ([]string, error) {
	var doc struct {
		RouteRules []struct {
			Target string `xml:"TargetEndpoint"`
		} `xml:"RouteRule"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse route targets: %w", err)
	}
	var targets []string
	for _, rule := range doc.RouteRules {
		name := strings.TrimSpace(rule.Target)
		if name == "" {
			continue
		}
		targets = append(targets, name)
	}
	return targets, nil
}

// ParseTargetEndpointServers extracts the TargetEndpoint name and load balancer server names.
func ParseTargetEndpointServers(data []byte) (string, []string, error) {
	var doc struct {
		XMLName xml.Name `xml:"TargetEndpoint"`
		Name    string   `xml:"name,attr"`
		HTTP    struct {
			LoadBalancer struct {
				Servers []struct {
					AttrName string `xml:"name,attr"`
					Name     string `xml:"Name"`
				} `xml:"Server"`
			} `xml:"LoadBalancer"`
		} `xml:"HTTPTargetConnection"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return "", nil, fmt.Errorf("parse target endpoint: %w", err)
	}

	var servers []string
	for _, server := range doc.HTTP.LoadBalancer.Servers {
		name := strings.TrimSpace(server.AttrName)
		if name == "" {
			name = strings.TrimSpace(server.Name)
		}
		if name == "" {
			continue
		}
		servers = append(servers, name)
	}
	return strings.TrimSpace(doc.Name), uniqueStrings(servers), nil
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
