package proxyxml

import (
	"encoding/xml"
	"fmt"
	"strings"

	"apigee/internal/util"
)

// TargetEndpointDetails captures HTTP target connection details for a TargetEndpoint.
type TargetEndpointDetails struct {
	Name         string
	URL          string
	LoadBalancer []string
	Properties   map[string]string
	SuccessCodes string
}

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
	details, err := ParseTargetEndpointDetails(data)
	if err != nil {
		return "", nil, err
	}
	return details.Name, details.LoadBalancer, nil
}

// ParseTargetEndpointDetails extracts TargetEndpoint name, URL, load balancer servers, and properties.
func ParseTargetEndpointDetails(data []byte) (TargetEndpointDetails, error) {
	var doc struct {
		XMLName xml.Name `xml:"TargetEndpoint"`
		Name    string   `xml:"name,attr"`
		HTTP    struct {
			URL          string `xml:"URL"`
			LoadBalancer struct {
				Servers []struct {
					AttrName string `xml:"name,attr"`
					Name     string `xml:"Name"`
				} `xml:"Server"`
			} `xml:"LoadBalancer"`
			Properties struct {
				Items []struct {
					Name  string `xml:"name,attr"`
					Value string `xml:",chardata"`
				} `xml:"Property"`
			} `xml:"Properties"`
		} `xml:"HTTPTargetConnection"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return TargetEndpointDetails{}, fmt.Errorf("parse target endpoint: %w", err)
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

	properties := make(map[string]string)
	for _, prop := range doc.HTTP.Properties.Items {
		key := strings.TrimSpace(prop.Name)
		if key == "" {
			continue
		}
		properties[key] = strings.TrimSpace(prop.Value)
	}
	successCodes := ""
	if value, ok := properties["success.codes"]; ok {
		successCodes = value
	} else {
		for key, value := range properties {
			if strings.EqualFold(key, "success.codes") {
				successCodes = value
				break
			}
		}
	}

	return TargetEndpointDetails{
		Name:         strings.TrimSpace(doc.Name),
		URL:          strings.TrimSpace(doc.HTTP.URL),
		LoadBalancer: util.UniqueStrings(servers),
		Properties:   properties,
		SuccessCodes: successCodes,
	}, nil
}
