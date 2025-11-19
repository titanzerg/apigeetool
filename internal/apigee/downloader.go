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

	"Apigee/internal/proxyxml"
)

// DownloadOptions contains parameters for downloading proxy endpoints.
type DownloadOptions struct {
	Host      string
	Org       string
	Proxy     string
	Token     string
	Revision  int
	OutputDir string
}

// DownloadProxyEndpoints downloads the selected Apigee proxy bundle and writes ProxyEndpoint XML files locally.
func DownloadProxyEndpoints(opts DownloadOptions) error {
	proxy := strings.TrimSpace(opts.Proxy)
	if proxy == "" {
		return fmt.Errorf("proxy name is required when -proxy flag is used")
	}

	org := strings.TrimSpace(opts.Org)
	if org == "" {
		return fmt.Errorf("Apigee organization is required (set -org or APIGEE_ORG)")
	}

	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return fmt.Errorf("Apigee OAuth token is required (set -token or APIGEE_TOKEN)")
	}

	client := NewClient(opts.Host, org, token)

	rev := opts.Revision
	if rev <= 0 {
		latest, err := client.latestRevision(proxy)
		if err != nil {
			return fmt.Errorf("resolve latest revision: %w", err)
		}
		if latest == 0 {
			return fmt.Errorf("no revisions found for proxy %s", proxy)
		}
		rev = latest
	}

	fmt.Printf("Downloading Apigee proxy %s revision %d...\n", proxy, rev)

	bundle, err := client.fetchProxyBundle(proxy, rev)
	if err != nil {
		return fmt.Errorf("fetch proxy bundle: %w", err)
	}

	dir := strings.TrimSpace(opts.OutputDir)
	if dir == "" {
		dir = "."
	}

	count, err := writeProxyEndpoints(bundle, dir)
	if err != nil {
		return err
	}

	fmt.Printf("Saved %d ProxyEndpoint file(s) to %s\n", count, dir)
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

func writeProxyEndpoints(bundle []byte, dir string) (int, error) {
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
		prefix, ok := proxyEndpointPrefix(name)
		if !ok {
			continue
		}

		if file.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".xml") {
			continue
		}

		rel := strings.TrimPrefix(name, prefix)
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
		return 0, fmt.Errorf("no ProxyEndpoint files found in Apigee bundle")
	}
	return written, nil
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
	Proxies []nameItem `json:"proxies"`
	Items   []nameItem `json:"items"`
	APIs    []nameItem `json:"apis"`
}

type nameItem struct {
	Name string `json:"name"`
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
		if len(names) > 0 {
			return names, nil
		}
	}

	var keyed map[string][]string
	if err := json.Unmarshal(data, &keyed); err == nil {
		for _, key := range []string{"apis", "items", "proxies"} {
			if names := keyed[key]; len(names) > 0 {
				return names, nil
			}
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
		if item.Name != "" {
			names = append(names, item.Name)
		}
	}
	return names
}

type bundleEndpoint struct {
	Name     string
	BasePath string
}

func parseProxyEndpointsFromBundle(bundle []byte) ([]bundleEndpoint, error) {
	readerAt := bytes.NewReader(bundle)
	zipReader, err := zip.NewReader(readerAt, int64(len(bundle)))
	if err != nil {
		return nil, fmt.Errorf("parse bundle zip: %w", err)
	}

	var endpoints []bundleEndpoint
	for _, file := range zipReader.File {
		name := file.Name
		prefix, ok := proxyEndpointPrefix(name)
		if !ok {
			continue
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".xml") {
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
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s in bundle: %w", name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		basePath, err := proxyxml.ExtractBasePath(data)
		if err != nil {
			return nil, fmt.Errorf("parse base path for %s: %w", name, err)
		}
		endpoints = append(endpoints, bundleEndpoint{
			Name:     endpointName,
			BasePath: basePath,
		})
	}

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no ProxyEndpoint files found in Apigee bundle")
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
