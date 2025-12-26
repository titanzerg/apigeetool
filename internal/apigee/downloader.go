package apigee

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"apigee/internal/proxyxml"
	"apigee/internal/util"
)

// DownloadOptions contains parameters for downloading proxy endpoints.
type DownloadOptions struct {
	Host      string
	Org       string
	Proxy     string
	Token     string
	Revision  int
	OutputDir string
	Quiet     bool
	// Artifact selection for DownloadProxyArtifacts.
	IncludeProxyEndpoints  bool
	IncludeTargetEndpoints bool
	IncludeResources       bool
	PreserveStructure      bool
}

// DownloadProxyEndpoints downloads the selected Apigee proxy bundle and writes ProxyEndpoint XML files locally.
func DownloadProxyEndpoints(opts DownloadOptions) error {
	bundle, _, err := downloadProxyBundle(opts)
	if err != nil {
		return err
	}
	dir := strings.TrimSpace(opts.OutputDir)
	if dir == "" {
		dir = "."
	}

	count, err := writeProxyArtifacts(bundle, dir, extractOptions{
		IncludeProxyEndpoints: true,
	})
	if err != nil {
		return err
	}
	if !opts.Quiet {
		fmt.Printf("Saved %d ProxyEndpoint file(s) to %s\n", count, dir)
	}
	return nil
}

// DownloadProxyArtifacts downloads a proxy bundle and writes selected artifacts locally.
func DownloadProxyArtifacts(opts DownloadOptions) error {
	bundle, _, err := downloadProxyBundle(opts)
	if err != nil {
		return err
	}

	dir := strings.TrimSpace(opts.OutputDir)
	if dir == "" {
		dir = "."
	}

	selection := extractOptions{
		IncludeProxyEndpoints:  opts.IncludeProxyEndpoints,
		IncludeTargetEndpoints: opts.IncludeTargetEndpoints,
		IncludeResources:       opts.IncludeResources,
		PreserveStructure:      opts.PreserveStructure,
	}
	if !selection.IncludeProxyEndpoints && !selection.IncludeTargetEndpoints && !selection.IncludeResources {
		selection.IncludeProxyEndpoints = true
	}

	count, err := writeProxyArtifacts(bundle, dir, selection)
	if err != nil {
		return err
	}

	if !opts.Quiet {
		fmt.Printf("Saved %d artifact file(s) to %s\n", count, dir)
	}
	return nil
}

// Client wraps Apigee Management API interactions needed by the CLI.
type Client struct {
	host       string
	org        string
	token      string
	httpClient *http.Client
}

// NewClient builds a Client with default configuration.
func NewClient(host, org, token string) *Client {
	host = strings.TrimSuffix(strings.TrimSpace(host), "/")
	if host == "" {
		host = "https://apigee.googleapis.com"
	}
	return &Client{
		host:       host,
		org:        strings.TrimSpace(org),
		token:      strings.TrimSpace(token),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Interface methods to allow test injection without exposing internal helpers.
func (c *Client) ListAPIs() ([]string, error) {
	return c.listAPIs()
}

func (c *Client) LatestRevision(proxy string) (int, error) {
	return c.latestRevision(proxy)
}

func (c *Client) FetchProxyBundle(proxy string, revision int) ([]byte, error) {
	return c.fetchProxyBundle(proxy, revision)
}

func (c *Client) ListEnvironments() ([]string, error) {
	return c.listEnvironments()
}

func (c *Client) EnvironmentsForRevision(proxy string, revision int) ([]string, error) {
	return c.environmentsForRevision(proxy, revision)
}

// DeployedRevisions returns the highest deployed revision per environment.
func (c *Client) DeployedRevisions(proxy string) (map[string]int, error) {
	return c.deployedRevisions(proxy)
}

func (c *Client) ListTargetServers(env string) ([]string, error) {
	return c.listTargetServers(env)
}

func (c *Client) FetchTargetServer(env, name string) (TargetServerRecord, error) {
	return c.fetchTargetServer(env, name)
}

func (c *Client) latestRevision(proxy string) (int, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apis/%s",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(proxy),
	)

	resp, err := c.doRequest(endpoint)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var payload struct {
		Revision []string `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode revision list: %w", err)
	}

	var maxRev int
	for _, revStr := range payload.Revision {
		rev, err := strconv.Atoi(revStr)
		if err != nil {
			continue
		}
		if rev > maxRev {
			maxRev = rev
		}
	}
	return maxRev, nil
}

func (c *Client) listAPIs() ([]string, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apis",
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
		return nil, fmt.Errorf("read apis list: %w", err)
	}
	names, err := decodeNameList(data)
	if err != nil {
		return nil, fmt.Errorf("decode apis list: %w", err)
	}
	return names, nil
}

func (c *Client) listProxyEndpoints(proxy string, revision int) ([]string, error) {
	endpoints := []string{
		fmt.Sprintf(
			"%s/v1/organizations/%s/apis/%s/revisions/%d/proxy-endpoints",
			c.host,
			url.PathEscape(c.org),
			url.PathEscape(proxy),
			revision,
		),
		fmt.Sprintf(
			"%s/v1/organizations/%s/apis/%s/revisions/%d/proxies",
			c.host,
			url.PathEscape(c.org),
			url.PathEscape(proxy),
			revision,
		),
	}

	var lastErr error
	for _, endpoint := range endpoints {
		resp, err := c.doRequestAny(http.MethodGet, endpoint, "application/json")
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("apigee proxy endpoints list failed: %s", extractError(resp))
			resp.Body.Close()
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read proxy endpoints list: %w", err)
		}
		names, err := decodeNameList(data)
		if err != nil {
			lastErr = err
			continue
		}
		return names, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("unable to list proxy endpoints for %s rev %d", proxy, revision)
}

func (c *Client) fetchProxyEndpointConfig(proxy string, revision int, endpointName string) ([]byte, error) {
	endpoints := []string{
		fmt.Sprintf(
			"%s/v1/organizations/%s/apis/%s/revisions/%d/proxy-endpoints/%s",
			c.host,
			url.PathEscape(c.org),
			url.PathEscape(proxy),
			revision,
			url.PathEscape(endpointName),
		),
		fmt.Sprintf(
			"%s/v1/organizations/%s/apis/%s/revisions/%d/proxies/%s",
			c.host,
			url.PathEscape(c.org),
			url.PathEscape(proxy),
			revision,
			url.PathEscape(endpointName),
		),
	}

	var lastErr error
	for _, endpoint := range endpoints {
		resp, err := c.doRequestAny(http.MethodGet, endpoint, "application/xml")
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("apigee proxy endpoint download failed: %s", extractError(resp))
			resp.Body.Close()
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read proxy endpoint body: %w", err)
		}
		return data, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("unable to fetch proxy endpoint %s.%s rev %d", proxy, endpointName, revision)
}

func (c *Client) environmentsForRevision(proxy string, revision int) ([]string, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apis/%s/deployments",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(proxy),
	)

	type deployment struct {
		Environment string          `json:"environment"`
		Revision    json.RawMessage `json:"revision"`
	}
	var payload struct {
		Deployments []deployment `json:"deployments"`
	}

	resp, err := c.doRequest(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode deployments: %w", err)
	}

	seen := make(map[string]struct{})
	var envs []string
	for _, dep := range payload.Deployments {
		env := strings.TrimSpace(dep.Environment)
		if env != "" {
			seen[env] = struct{}{}
		}
		for _, rev := range parseRevisionEntries(dep.Revision) {
			num, _ := strconv.Atoi(strings.TrimSpace(rev.Name))
			if num == revision {
				state := strings.TrimSpace(strings.ToLower(rev.State))
				if state == "" || state == "deployed" {
					envs = append(envs, env)
					break
				}
			}
		}
	}
	if len(envs) == 0 && len(seen) > 0 {
		for env := range seen {
			if env == "" {
				continue
			}
			envs = append(envs, env)
		}
	}
	return envs, nil
}

func (c *Client) deployedRevisions(proxy string) (map[string]int, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apis/%s/deployments",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(proxy),
	)

	type deployment struct {
		Environment string          `json:"environment"`
		Revision    json.RawMessage `json:"revision"`
	}
	var payload struct {
		Deployments []deployment `json:"deployments"`
	}

	resp, err := c.doRequest(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode deployments: %w", err)
	}

	results := make(map[string]int)
	for _, dep := range payload.Deployments {
		env := strings.TrimSpace(dep.Environment)
		if env == "" {
			continue
		}
		for _, rev := range parseRevisionEntries(dep.Revision) {
			state := strings.TrimSpace(strings.ToLower(rev.State))
			if state != "" && state != "deployed" {
				continue
			}
			num, err := strconv.Atoi(strings.TrimSpace(rev.Name))
			if err != nil {
				continue
			}
			if num > results[env] {
				results[env] = num
			}
		}
	}
	return results, nil
}

type revisionEntry struct {
	Name  string
	State string
}

func parseRevisionEntries(raw json.RawMessage) []revisionEntry {
	if len(raw) == 0 {
		return nil
	}
	var items []json.RawMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &items); err != nil {
			// fallback: treat whole value as single entry
			if rev, ok := parseRevisionEntry(raw); ok {
				return []revisionEntry{rev}
			}
			return nil
		}
	} else {
		items = append(items, raw)
	}

	var result []revisionEntry
	for _, item := range items {
		if rev, ok := parseRevisionEntry(item); ok {
			result = append(result, rev)
		}
	}
	return result
}

func parseRevisionEntry(raw json.RawMessage) (revisionEntry, bool) {
	if len(raw) == 0 {
		return revisionEntry{}, false
	}

	// String or number: revision only
	if raw[0] == '"' || raw[0] == '\'' || (raw[0] >= '0' && raw[0] <= '9') {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return revisionEntry{Name: s}, true
		}
	}

	// Object with name/state
	var obj struct {
		Name  string `json:"name"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if strings.TrimSpace(obj.Name) == "" {
			return revisionEntry{}, false
		}
		return revisionEntry{Name: obj.Name, State: obj.State}, true
	}

	return revisionEntry{}, false
}

func (c *Client) listEnvironments() ([]string, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/environments",
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
		return nil, fmt.Errorf("read environments: %w", err)
	}
	names, err := decodeNameList(data)
	if err != nil {
		return nil, fmt.Errorf("decode environments: %w", err)
	}
	return names, nil
}

func (c *Client) fetchProxyBundle(proxy string, revision int) ([]byte, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apis/%s/revisions/%d?format=bundle",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(proxy),
		revision,
	)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("apigee bundle download failed: %s", extractError(resp))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read bundle body: %w", err)
	}
	return data, nil
}

func (c *Client) doRequest(endpoint string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body := extractError(resp)
		return nil, fmt.Errorf("apigee request failed: %s", body)
	}
	return resp, nil
}

func extractError(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || len(body) == 0 {
		return resp.Status
	}
	return fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
}

type extractOptions struct {
	IncludeProxyEndpoints  bool
	IncludeTargetEndpoints bool
	IncludeResources       bool
	PreserveStructure      bool
}

func writeProxyArtifacts(bundle []byte, dir string, opts extractOptions) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create output dir: %w", err)
	}

	readerAt := bytes.NewReader(bundle)
	zipReader, err := zip.NewReader(readerAt, int64(len(bundle)))
	if err != nil {
		return 0, fmt.Errorf("parse bundle zip: %w", err)
	}

	var written int
	for _, file := range zipReader.File {
		name := file.Name
		kind, prefix, ok := classifyArtifact(name, opts)
		if !ok {
			continue
		}

		if file.FileInfo().IsDir() {
			continue
		}

		if kind != artifactResource && !strings.HasSuffix(strings.ToLower(name), ".xml") {
			continue
		}

		rel := buildArtifactRelPath(name, prefix, opts.PreserveStructure)
		outPath := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return written, fmt.Errorf("ensure output dir for %s: %w", outPath, err)
		}

		rc, err := file.Open()
		if err != nil {
			return written, fmt.Errorf("open %s in bundle: %w", name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return written, fmt.Errorf("read %s: %w", name, err)
		}

		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", outPath, err)
		}
		written++
	}

	if written == 0 {
		return 0, fmt.Errorf("no matching artifacts found in Apigee bundle")
	}
	return written, nil
}

type artifactKind int

const (
	artifactProxyEndpoint artifactKind = iota
	artifactTargetEndpoint
	artifactResource
)

func classifyArtifact(name string, opts extractOptions) (artifactKind, string, bool) {
	if prefix, ok := proxyEndpointPrefix(name); ok {
		if !opts.IncludeProxyEndpoints {
			return artifactProxyEndpoint, "", false
		}
		return artifactProxyEndpoint, prefix, true
	}
	if prefix, ok := targetEndpointPrefix(name); ok {
		if !opts.IncludeTargetEndpoints {
			return artifactTargetEndpoint, "", false
		}
		return artifactTargetEndpoint, prefix, true
	}
	if strings.HasPrefix(name, "apiproxy/resources/") {
		if !opts.IncludeResources {
			return artifactResource, "", false
		}
		return artifactResource, "apiproxy/resources/", true
	}
	return artifactProxyEndpoint, "", false
}

func buildArtifactRelPath(name, prefix string, preserveStructure bool) string {
	if preserveStructure && strings.HasPrefix(name, "apiproxy/") {
		return strings.TrimPrefix(name, "apiproxy/")
	}
	return strings.TrimPrefix(name, prefix)
}

func proxyEndpointPrefix(name string) (string, bool) {
	prefixes := []string{
		"apiproxy/proxies/",
		"apiproxy/proxy-endpoints/",
		"apiproxy/proxy_endpoints/",
		"apiproxy/proxy-endpoint/",
		"apiproxy/proxy_endpoint/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return prefix, true
		}
	}
	return "", false
}

func targetEndpointPrefix(name string) (string, bool) {
	prefixes := []string{
		"apiproxy/targets/",
		"apiproxy/target-endpoints/",
		"apiproxy/target_endpoints/",
		"apiproxy/target-endpoint/",
		"apiproxy/target_endpoint/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return prefix, true
		}
	}
	return "", false
}

func downloadProxyBundle(opts DownloadOptions) ([]byte, int, error) {
	proxy := strings.TrimSpace(opts.Proxy)
	if proxy == "" {
		return nil, 0, fmt.Errorf("proxy name is required when -proxy flag is used")
	}

	org := strings.TrimSpace(opts.Org)
	if org == "" {
		return nil, 0, fmt.Errorf("Apigee organization is required (set -org or APIGEE_ORG)")
	}

	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return nil, 0, fmt.Errorf("Apigee OAuth token is required (set -token or APIGEE_TOKEN)")
	}

	client := NewClient(opts.Host, org, token)

	rev := opts.Revision
	if rev <= 0 {
		latest, err := client.latestRevision(proxy)
		if err != nil {
			return nil, 0, fmt.Errorf("resolve latest revision: %w", err)
		}
		if latest == 0 {
			return nil, 0, fmt.Errorf("no revisions found for proxy %s", proxy)
		}
		rev = latest
	}

	if !opts.Quiet {
		fmt.Printf("Downloading Apigee proxy %s revision %d...\n", proxy, rev)
	}

	bundle, err := client.fetchProxyBundle(proxy, rev)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch proxy bundle: %w", err)
	}

	return bundle, rev, nil
}

func (c *Client) fetchTargetServer(env, name string) (TargetServerRecord, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/environments/%s/targetservers/%s",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(env),
		url.PathEscape(name),
	)

	resp, err := c.doRequest(endpoint)
	if err != nil {
		return TargetServerRecord{}, err
	}
	defer resp.Body.Close()

	var payload struct {
		Name    string `json:"name"`
		Host    string `json:"host"`
		Port    int    `json:"port"`
		SSLInfo struct {
			Enabled bool `json:"enabled"`
		} `json:"sSLInfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return TargetServerRecord{}, fmt.Errorf("decode target server: %w", err)
	}

	host := strings.TrimSpace(payload.Host)
	port := payload.Port
	isSSL := payload.SSLInfo.Enabled
	scheme := "http"
	if isSSL || port == 443 {
		scheme = "https"
	}
	urlVal := fmt.Sprintf("%s://%s", scheme, host)
	if port > 0 && port != 80 && port != 443 {
		urlVal = fmt.Sprintf("%s://%s:%d", scheme, host, port)
	}

	return TargetServerRecord{
		Name:        strings.TrimSpace(payload.Name),
		Environment: env,
		Host:        host,
		Port:        port,
		IsSSL:       isSSL,
		URL:         urlVal,
	}, nil
}

func (c *Client) listTargetServers(env string) ([]string, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/environments/%s/targetservers",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(env),
	)
	resp, err := c.doRequest(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read target servers: %w", err)
	}
	names, err := decodeNameList(data)
	if err != nil {
		return nil, fmt.Errorf("decode target servers: %w", err)
	}
	return names, nil
}

func (c *Client) doRequestAny(method, endpoint, accept string) (*http.Response, error) {
	req, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	return c.httpClient.Do(req)
}

type nameContainer struct {
	Proxies     []nameItem `json:"proxies"`
	Items       []nameItem `json:"items"`
	APIs        []nameItem `json:"apis"`
	APIProducts []nameItem `json:"apiProduct"`
	Apps        []nameItem `json:"app"`
	AppsList    []nameItem `json:"apps"`
	AppIDs      []string   `json:"appIds"`
}

type nameItem struct {
	Name  string `json:"name"`
	AppID string `json:"appId"`
}

func decodeNameList(data []byte) ([]string, error) {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}

	var container nameContainer
	if err := json.Unmarshal(data, &container); err == nil {
		names := collectNames(container.Proxies)
		names = append(names, collectNames(container.Items)...)
		names = append(names, collectNames(container.APIs)...)
		names = append(names, collectNames(container.APIProducts)...)
		names = append(names, collectNames(container.Apps)...)
		names = append(names, collectNames(container.AppsList)...)
		names = append(names, container.AppIDs...)
		if len(names) > 0 {
			return names, nil
		}
	}

	var keyed map[string][]string
	if err := json.Unmarshal(data, &keyed); err == nil {
		for _, key := range []string{"apis", "items", "proxies", "apiProduct", "app", "apps", "appIds", "appids"} {
			if names := keyed[key]; len(names) > 0 {
				return names, nil
			}
		}
	}

	// Fallback: array of objects with name/appId/id fields.
	var objects []map[string]interface{}
	if err := json.Unmarshal(data, &objects); err == nil {
		var names []string
		for _, obj := range objects {
			if name := extractNameish(obj); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			return names, nil
		}
	}

	return nil, fmt.Errorf("unsupported response: %s", strings.TrimSpace(string(data)))
}

func collectNames(items []nameItem) []string {
	if len(items) == 0 {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimSpace(item.AppID)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func extractNameish(obj map[string]interface{}) string {
	for _, key := range []string{"name", "appId", "appID", "app_id", "app_name", "appName", "id"} {
		if v, ok := obj[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

type bundleEndpoint struct {
	Name          string
	BasePath      string
	TargetServers []string
	FlowCount     int
}

func parseProxyEndpointsFromBundle(bundle []byte) ([]bundleEndpoint, error) {
	readerAt := bytes.NewReader(bundle)
	zipReader, err := zip.NewReader(readerAt, int64(len(bundle)))
	if err != nil {
		return nil, fmt.Errorf("parse bundle zip: %w", err)
	}

	type proxyMeta struct {
		basePath     string
		targetRoutes []string
		flowCount    int
	}

	targetServers := make(map[string][]string)
	proxyData := make(map[string]proxyMeta)

	for _, file := range zipReader.File {
		name := file.Name
		if file.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".xml") {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s in bundle: %w", name, err)
		}
		data, readErr := io.ReadAll(rc)
		rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", name, readErr)
		}

		if targetPrefix, ok := targetEndpointPrefix(name); ok {
			rel := strings.TrimPrefix(name, targetPrefix)
			if rel == "" {
				continue
			}
			targetName, servers, err := proxyxml.ParseTargetEndpointServers(data)
			if err != nil {
				return nil, fmt.Errorf("parse target endpoint %s: %w", name, err)
			}
			if targetName == "" {
				targetName = strings.TrimSuffix(rel, path.Ext(rel))
			}
			if targetName == "" {
				continue
			}
			key := strings.ToLower(targetName)
			targetServers[key] = util.MergeAndUnique(targetServers[key], servers)
			continue
		}

		prefix, ok := proxyEndpointPrefix(name)
		if !ok {
			continue
		}

		rel := strings.TrimPrefix(name, prefix)
		rel = strings.TrimLeft(rel, "/")
		if rel == "" {
			continue
		}
		segment := rel
		if idx := strings.Index(segment, "/"); idx >= 0 {
			segment = segment[:idx]
		}
		endpointName := strings.TrimSuffix(segment, path.Ext(segment))

		flows, err := proxyxml.ParseFlows(data)
		if err != nil {
			return nil, fmt.Errorf("parse flows for %s: %w", name, err)
		}
		basePath, err := proxyxml.ExtractBasePath(data)
		if err != nil {
			return nil, fmt.Errorf("parse base path for %s: %w", name, err)
		}
		targets, err := proxyxml.ExtractRouteTargets(data)
		if err != nil {
			return nil, fmt.Errorf("parse route targets for %s: %w", name, err)
		}
		proxyData[endpointName] = proxyMeta{
			basePath:     basePath,
			targetRoutes: targets,
			flowCount:    len(flows),
		}
	}

	if len(proxyData) == 0 {
		return nil, fmt.Errorf("no ProxyEndpoint files found in Apigee bundle")
	}

	var endpoints []bundleEndpoint
	for name, meta := range proxyData {
		var servers []string
		for _, tgt := range meta.targetRoutes {
			key := strings.ToLower(strings.TrimSpace(tgt))
			if key == "" {
				continue
			}
			servers = append(servers, targetServers[key]...)
		}
		endpoints = append(endpoints, bundleEndpoint{
			Name:          name,
			BasePath:      meta.basePath,
			TargetServers: util.MergeAndUnique(nil, servers),
			FlowCount:     meta.flowCount,
		})
	}

	return endpoints, nil
}

// FindClosestProxyEndpoint finds the downloaded ProxyEndpoint XML that is most similar to the generated file.
func FindClosestProxyEndpoint(generatedPath, dir string) (string, float64, error) {
	generatedPath = strings.TrimSpace(generatedPath)
	if generatedPath == "" {
		return "", 0, fmt.Errorf("generated ProxyEndpoint path is empty")
	}

	baseData, err := os.ReadFile(generatedPath)
	if err != nil {
		return "", 0, fmt.Errorf("read generated ProxyEndpoint: %w", err)
	}
	baseFlows, err := proxyxml.ParseFlows(baseData)
	if err != nil {
		return "", 0, fmt.Errorf("parse generated flows: %w", err)
	}
	baseSignatures := flowSignatures(baseFlows)

	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}

	info, err := os.Stat(dir)
	if err != nil {
		return "", 0, fmt.Errorf("stat download dir: %w", err)
	}
	if !info.IsDir() {
		return "", 0, fmt.Errorf("download path is not a directory: %s", dir)
	}

	var bestFile string
	var bestScore float64

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".xml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		flows, err := proxyxml.ParseFlows(data)
		if err != nil {
			return fmt.Errorf("parse flows in %s: %w", path, err)
		}
		score := flowSimilarity(baseSignatures, flowSignatures(flows))
		if score > bestScore {
			bestScore = score
			bestFile = path
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	if bestFile == "" {
		return "", 0, fmt.Errorf("no XML files found in %s", dir)
	}
	return bestFile, bestScore, nil
}

func flowSignatures(flows []proxyxml.Flow) []string {
	if len(flows) == 0 {
		return nil
	}
	signatures := make([]string, 0, len(flows))
	for _, fl := range flows {
		name := strings.TrimSpace(fl.Name)
		condition := normalizeFlowField(fl.Condition)
		description := normalizeFlowField(fl.Description)
		signatures = append(signatures, fmt.Sprintf("%s|%s|%s", name, condition, description))
	}
	return signatures
}

func normalizeFlowField(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

func flowSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}

	countA := make(map[string]int, len(a))
	for _, val := range a {
		countA[val]++
	}

	countB := make(map[string]int, len(b))
	for _, val := range b {
		countB[val]++
	}

	var matches int
	for sig, aCount := range countA {
		if bCount := countB[sig]; bCount > 0 {
			if aCount < bCount {
				matches += aCount
			} else {
				matches += bCount
			}
		}
	}

	total := len(a) + len(b)
	if total == 0 {
		return 0
	}
	return float64(2*matches) / float64(total)
}
