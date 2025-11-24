package apigee

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// CollectAPIProductsOptions controls the behavior of CollectAPIProducts.
type CollectAPIProductsOptions struct {
	Host  string
	Org   string
	Token string
}

// APIProductRecord holds the minimal set of API product data to sync.
type APIProductRecord struct {
	Name         string
	Environments []string
	Proxies      []string
	Apps         []string
}

// CollectAPIProducts fetches all API products along with environments, proxies, and apps.
func CollectAPIProducts(opts CollectAPIProductsOptions) ([]APIProductRecord, error) {
	org := strings.TrimSpace(opts.Org)
	token := strings.TrimSpace(opts.Token)
	host := strings.TrimSpace(opts.Host)

	if org == "" || token == "" {
		return nil, fmt.Errorf("Apigee org and token are required")
	}

	client := NewClient(host, org, token)
	names, err := client.listAPIProducts()
	if err != nil {
		return nil, fmt.Errorf("list api products: %w", err)
	}

	var records []APIProductRecord
	for _, name := range names {
		product, err := client.fetchAPIProduct(name)
		if err != nil {
			return nil, fmt.Errorf("fetch api product %s: %w", name, err)
		}
		apps, err := client.fetchAPIProductApps(name)
		if err != nil {
			return nil, fmt.Errorf("fetch api product apps %s: %w", name, err)
		}
		records = append(records, APIProductRecord{
			Name:         product.Name,
			Environments: uniqueSorted(product.Environments),
			Proxies:      uniqueSorted(product.Proxies),
			Apps:         uniqueSorted(apps),
		})
	}

	return records, nil
}

func (c *Client) listAPIProducts() ([]string, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apiproducts",
		c.host,
		url.PathEscape(c.org),
	)
	resp, err := c.doRequest(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read api products: %w", err)
	}
	names, err := decodeNameList(data)
	if err != nil {
		return nil, fmt.Errorf("decode api products: %w", err)
	}
	return names, nil
}

type apiProductDetail struct {
	Name         string   `json:"name"`
	Environments []string `json:"environments"`
	Proxies      []string `json:"proxies"`
}

func (c *Client) fetchAPIProduct(name string) (apiProductDetail, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apiproducts/%s",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(name),
	)
	resp, err := c.doRequest(endpoint)
	if err != nil {
		return apiProductDetail{}, err
	}
	defer resp.Body.Close()

	var payload apiProductDetail
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return apiProductDetail{}, fmt.Errorf("decode api product: %w", err)
	}
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Environments = trimStrings(payload.Environments)
	payload.Proxies = trimStrings(payload.Proxies)
	return payload, nil
}

func (c *Client) fetchAPIProductApps(name string) ([]string, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apiproducts/%s/apps",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(name),
	)
	resp, err := c.doRequestAny(http.MethodGet, endpoint, "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []string{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("apigee request failed: %s", extractError(resp))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read api product apps: %w", err)
	}
	names, err := decodeNameList(data)
	if err != nil {
		return nil, fmt.Errorf("decode api product apps: %w", err)
	}
	return trimStrings(names), nil
}

func trimStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		lower := strings.ToLower(v)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
