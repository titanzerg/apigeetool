package apigee

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"apigee/internal/util"
)

// CollectAPIProductsOptions controls the behavior of CollectAPIProducts.
type CollectAPIProductsOptions struct {
	Host     string
	Org      string
	Token    string
	Client   ManagementClient
	Progress func(APIProductProgress)
}

// APIProductRecord holds the minimal set of API product data to sync.
type APIProductRecord struct {
	Name         string
	Environments []string
	Proxies      []string
	Apps         []string
}

// APIProductProgress describes the progress of fetching an API product.
type APIProductProgress struct {
	Index        int
	Total        int
	Name         string
	Environments []string
	Proxies      []string
	Apps         int
	Err          error
}

// CollectAPIProducts fetches all API products along with environments, proxies, and apps.
func CollectAPIProducts(opts CollectAPIProductsOptions) ([]APIProductRecord, error) {
	client, err := resolveClient(opts.Client, opts.Host, opts.Org, opts.Token)
	if err != nil {
		return nil, err
	}
	debug := isSyncDebug()

	names, err := client.ListAPIProducts()
	if err != nil {
		return nil, fmt.Errorf("list api products: %w", err)
	}

	appsByProduct, err := collectAppsByProduct(client, debug)
	if err != nil {
		return nil, fmt.Errorf("list apps for products: %w", err)
	}
	debugLog(debug, "org-wide app map entries: %d\n", len(appsByProduct))

	var records []APIProductRecord
	for i, name := range names {
		product, err := client.FetchAPIProduct(name)
		if err != nil {
			reportProductProgress(opts.Progress, APIProductProgress{
				Index: i + 1,
				Total: len(names),
				Name:  name,
				Err:   err,
			})
			return nil, fmt.Errorf("fetch api product %s: %w", name, err)
		}
		apps := uniqueSorted(appsByProduct[strings.ToLower(product.Name)])
		// Fallback: if org-wide listing didn't return apps for this product, fetch per product.
		if len(apps) == 0 {
			debugLog(debug, "fallback fetch apps for product %s (org map empty)\n", product.Name)
			productApps, appErr := client.FetchAPIProductApps(product.Name)
			if appErr != nil {
				reportProductProgress(opts.Progress, APIProductProgress{
					Index:        i + 1,
					Total:        len(names),
					Name:         name,
					Environments: product.Environments,
					Proxies:      product.Proxies,
					Err:          appErr,
				})
				return nil, fmt.Errorf("fetch api product apps %s: %w", name, appErr)
			}
			apps = uniqueSorted(productApps)
			debugLog(debug, "fallback apps for product %s: %d\n", product.Name, len(apps))
		}
		record := APIProductRecord{
			Name:         product.Name,
			Environments: util.UniqueSortedStrings(product.Environments),
			Proxies:      util.UniqueSortedStrings(product.Proxies),
			Apps:         apps,
		}
		records = append(records, record)
		reportProductProgress(opts.Progress, APIProductProgress{
			Index:        i + 1,
			Total:        len(names),
			Name:         record.Name,
			Environments: record.Environments,
			Proxies:      record.Proxies,
			Apps:         len(record.Apps),
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
	Name           string          `json:"name"`
	Environments   []string        `json:"environments"`
	Proxies        []string        `json:"proxies"`
	OperationGroup apiOperationSet `json:"operationGroup"`
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
	payload.Proxies = mergeProductProxies(payload.Proxies, payload.OperationGroup)
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

// apiOperationSet captures the operationGroup section that lists proxies indirectly.
type apiOperationSet struct {
	OperationConfigs []apiOperationConfig `json:"operationConfigs"`
}

type apiOperationConfig struct {
	APIProxy  string `json:"apiProxy"`
	APISource string `json:"apiSource"`
}

func mergeProductProxies(proxies []string, group apiOperationSet) []string {
	combined := append(trimStrings(proxies), extractOperationGroupProxies(group)...)
	return uniqueSorted(combined)
}

func extractOperationGroupProxies(group apiOperationSet) []string {
	if len(group.OperationConfigs) == 0 {
		return nil
	}
	proxies := make([]string, 0, len(group.OperationConfigs))
	for _, cfg := range group.OperationConfigs {
		proxy := strings.TrimSpace(cfg.APIProxy)
		if proxy == "" {
			proxy = strings.TrimSpace(cfg.APISource)
		}
		if proxy == "" {
			continue
		}
		proxies = append(proxies, proxy)
	}
	return uniqueSorted(proxies)
}

func reportProductProgress(fn func(APIProductProgress), progress APIProductProgress) {
	if fn == nil {
		return
	}
	fn(progress)
}

type orgAppsPage struct {
	Apps         []orgApp `json:"app"`
	NextStartKey string   `json:"nextStartKey"`
}

type orgApp struct {
	Name           string              `json:"name"`
	AppName        string              `json:"appName"`
	AppID          string              `json:"appId"`
	AppOwner       string              `json:"appOwner"`
	DeveloperID    string              `json:"developerId"`
	DeveloperEmail string              `json:"developerEmail"`
	CreatedAt      epochMillis         `json:"createdAt"`
	Attributes     []nameValue         `json:"attributes"`
	APIProducts    apiProductNames     `json:"apiProducts"`
	Credentials    []orgAppCredentials `json:"credentials"`
}

type orgAppCredentials struct {
	APIProducts    apiProductNames `json:"apiProducts"`
	ConsumerKey    string          `json:"consumerKey"`
	ConsumerSecret string          `json:"consumerSecret"`
	ExpiresAt      epochMillis     `json:"expiresAt"`
}

type apiProductNames []string

type nameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// UnmarshalJSON accepts either an array of strings or an array of objects with apiproduct/apiProduct/name keys.
func (p *apiProductNames) UnmarshalJSON(data []byte) error {
	var stringsOnly []string
	if err := json.Unmarshal(data, &stringsOnly); err == nil {
		*p = normalizeProductNames(stringsOnly)
		return nil
	}

	var objs []map[string]interface{}
	if err := json.Unmarshal(data, &objs); err != nil {
		// Unknown shape; ignore silently.
		return nil
	}
	var names []string
	for _, obj := range objs {
		for _, key := range []string{"apiproduct", "apiProduct", "name"} {
			if v, ok := obj[key]; ok {
				if s, ok := v.(string); ok {
					names = append(names, s)
					break
				}
			}
		}
	}
	*p = normalizeProductNames(names)
	return nil
}

func normalizeProductNames(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func collectAppsByProduct(client ManagementClient, debug bool) (map[string][]string, error) {
	appsByProduct := make(map[string][]string)
	startKey := ""
	for {
		page, err := client.ListOrganizationApps(startKey)
		if err != nil {
			return nil, err
		}
		debugLog(debug, "apps page: %d apps (nextStartKey=%q)\n", len(page.Apps), page.NextStartKey)
		applyAppsToProductMap(page.Apps, appsByProduct)
		if next := strings.TrimSpace(page.NextStartKey); next == "" {
			break
		} else {
			startKey = next
		}
	}
	finalizeAppsByProduct(appsByProduct)
	return appsByProduct, nil
}

func applyAppsToProductMap(apps []orgApp, appsByProduct map[string][]string) {
	for _, app := range apps {
		name := strings.TrimSpace(firstNonEmpty(app.AppName, app.Name, app.AppID))
		if name == "" {
			continue
		}
		products := mergeAppProducts(app)
		for _, prod := range products {
			key := strings.ToLower(strings.TrimSpace(prod))
			if key == "" {
				continue
			}
			appsByProduct[key] = append(appsByProduct[key], name)
		}
	}
}

func finalizeAppsByProduct(appsByProduct map[string][]string) {
	for prod, names := range appsByProduct {
		appsByProduct[prod] = uniqueSorted(names)
	}
}

func (c *Client) listOrganizationApps(startKey string) (orgAppsPage, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apps?expand=true",
		c.host,
		url.PathEscape(c.org),
	)
	if startKey = strings.TrimSpace(startKey); startKey != "" {
		endpoint += "&startKey=" + url.QueryEscape(startKey)
	}
	resp, err := c.doRequestAny(http.MethodGet, endpoint, "application/json")
	if err != nil {
		return orgAppsPage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return orgAppsPage{}, fmt.Errorf("list organization apps failed: %s", extractError(resp))
	}

	var payload orgAppsPage
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return orgAppsPage{}, fmt.Errorf("decode organization apps: %w", err)
	}
	for i := range payload.Apps {
		payload.Apps[i].APIProducts = trimStrings(payload.Apps[i].APIProducts)
		for j := range payload.Apps[i].APIProducts {
			payload.Apps[i].APIProducts[j] = strings.ToLower(payload.Apps[i].APIProducts[j])
		}
	}
	return payload, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func isSyncDebug() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APIGEE_SYNC_DEBUG")), "true")
}

func debugLog(enabled bool, format string, args ...interface{}) {
	if !enabled {
		return
	}
	fmt.Printf("[sync-debug] "+format, args...)
}

func mergeAppProducts(app orgApp) []string {
	products := util.MergeAndUnique(nil, app.APIProducts)
	for _, cred := range app.Credentials {
		products = util.MergeAndUnique(products, cred.APIProducts)
	}
	products = normalizeProductNames(products)
	return uniqueSorted(products)
}

// Interface shims for dependency injection.
func (c *Client) ListAPIProducts() ([]string, error) {
	return c.listAPIProducts()
}

func (c *Client) FetchAPIProduct(name string) (apiProductDetail, error) {
	return c.fetchAPIProduct(name)
}

func (c *Client) ListOrganizationApps(startKey string) (orgAppsPage, error) {
	return c.listOrganizationApps(startKey)
}

func (c *Client) FetchAPIProductApps(name string) ([]string, error) {
	return c.fetchAPIProductApps(name)
}
