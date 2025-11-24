package apigee

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"apigee/internal/util"
)

// CollectAPIProductsOptions controls the behavior of CollectAPIProducts.
type CollectAPIProductsOptions struct {
	Host   string
	Org    string
	Token  string
	Client ManagementClient
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
	client, err := resolveClient(opts.Client, opts.Host, opts.Org, opts.Token)
	if err != nil {
		return nil, err
	}

	names, err := client.ListAPIProducts()
	if err != nil {
		return nil, fmt.Errorf("list api products: %w", err)
	}

	var records []APIProductRecord
	for _, name := range names {
		product, err := client.FetchAPIProduct(name)
		if err != nil {
			return nil, fmt.Errorf("fetch api product %s: %w", name, err)
		}
		apps, err := client.FetchAPIProductApps(name)
		if err != nil {
			return nil, fmt.Errorf("fetch api product apps %s: %w", name, err)
		}
		records = append(records, APIProductRecord{
			Name:         product.Name,
			Environments: util.UniqueSortedStrings(product.Environments),
			Proxies:      util.UniqueSortedStrings(product.Proxies),
			Apps:         util.UniqueSortedStrings(apps),
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
	return util.TrimStrings(values)
}

func uniqueSorted(values []string) []string {
	return util.UniqueSortedStrings(values)
}

// Interface shims for dependency injection.
func (c *Client) ListAPIProducts() ([]string, error) {
	return c.listAPIProducts()
}

func (c *Client) FetchAPIProduct(name string) (apiProductDetail, error) {
	return c.fetchAPIProduct(name)
}

func (c *Client) FetchAPIProductApps(name string) ([]string, error) {
	return c.fetchAPIProductApps(name)
}
