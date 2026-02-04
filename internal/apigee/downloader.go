package apigee

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"apigee/internal/proxyxml"
	"apigee/internal/util"
)

const bearerPrefix = "Bearer "

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
	IncludePolicies        bool
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
		IncludePolicies:        opts.IncludePolicies,
		IncludeResources:       opts.IncludeResources,
		PreserveStructure:      opts.PreserveStructure,
	}
	if !selection.IncludeProxyEndpoints && !selection.IncludeTargetEndpoints && !selection.IncludePolicies && !selection.IncludeResources {
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
	tokenMu    sync.Mutex
	tokenFn    TokenSupplier
	httpClient *http.Client
}

// TokenSupplier returns a fresh bearer token.
type TokenSupplier func() (string, error)

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
		tokenFn:    tokenSupplierFromEnv(),
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

func (c *Client) environmentsForRevision(proxy string, revision int) ([]string, error) {
	deployments, err := c.fetchDeployments(proxy)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var envs []string
	for _, dep := range deployments {
		env := strings.TrimSpace(dep.Environment)
		if env != "" {
			seen[env] = struct{}{}
		}
		if hasRevision(dep.Revision, revision) {
			envs = append(envs, env)
		}
	}
	if len(envs) == 0 && len(seen) > 0 {
		envs = uniqueEnvList(seen)
	}
	return envs, nil
}

func (c *Client) deployedRevisions(proxy string) (map[string]int, error) {
	deployments, err := c.fetchDeployments(proxy)
	if err != nil {
		return nil, err
	}

	results := make(map[string]int)
	for _, dep := range deployments {
		env := strings.TrimSpace(dep.Environment)
		if env == "" {
			continue
		}
		updateMaxRevision(results, env, dep.Revision)
	}
	return results, nil
}

type deploymentRecord struct {
	Environment string          `json:"environment"`
	Revision    json.RawMessage `json:"revision"`
}

func (c *Client) fetchDeployments(proxy string) ([]deploymentRecord, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apis/%s/deployments",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(proxy),
	)
	var payload struct {
		Deployments []deploymentRecord `json:"deployments"`
	}

	resp, err := c.doRequest(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode deployments: %w", err)
	}
	return payload.Deployments, nil
}

func hasRevision(raw json.RawMessage, target int) bool {
	for _, rev := range parseRevisionEntries(raw) {
		num, _ := strconv.Atoi(strings.TrimSpace(rev.Name))
		if num != target {
			continue
		}
		state := strings.TrimSpace(strings.ToLower(rev.State))
		if state == "" || state == "deployed" {
			return true
		}
	}
	return false
}

func updateMaxRevision(results map[string]int, env string, raw json.RawMessage) {
	for _, rev := range parseRevisionEntries(raw) {
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

func uniqueEnvList(seen map[string]struct{}) []string {
	envs := make([]string, 0, len(seen))
	for env := range seen {
		if env == "" {
			continue
		}
		envs = append(envs, env)
	}
	return envs
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

	resp, err := c.doRequestWithAccept(endpoint, "application/octet-stream")
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
	return c.doRequestWithAccept(endpoint, "application/json")
}

func (c *Client) doRequestWithAccept(endpoint, accept string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", bearerPrefix+c.token)
	req.Header.Set("Accept", accept)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && c.tokenFn != nil {
		resp.Body.Close()
		if err := c.refreshToken(); err != nil {
			return nil, fmt.Errorf("refresh token: %w", err)
		}
		req, err = http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", bearerPrefix+c.token)
		req.Header.Set("Accept", accept)
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body := extractError(resp)
		return nil, fmt.Errorf("apigee request failed: %s", body)
	}
	return resp, nil
}

func (c *Client) refreshToken() error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.tokenFn == nil {
		return errors.New("no token refresh command configured")
	}
	token, err := c.tokenFn()
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token refresh returned empty token")
	}
	c.token = token
	return nil
}

func tokenSupplierFromEnv() TokenSupplier {
	command := strings.TrimSpace(os.Getenv("APIGEE_TOKEN_COMMAND"))
	if command == "" {
		command = strings.TrimSpace(os.Getenv("APIGEE_TOKEN_CMD"))
	}
	if command == "" {
		return nil
	}
	return func() (string, error) {
		out, err := exec.Command("sh", "-c", command).Output()
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
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
	IncludePolicies        bool
	IncludeResources       bool
	PreserveStructure      bool
}

func writeProxyArtifacts(bundle []byte, dir string, opts extractOptions) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create output dir: %w", err)
	}

	zipReader, err := newZipReader(bundle)
	if err != nil {
		return 0, err
	}

	written, err := writeBundleArtifacts(zipReader.File, dir, opts)
	if err != nil {
		return written, err
	}

	if written == 0 {
		return 0, fmt.Errorf("no matching artifacts found in Apigee bundle")
	}
	return written, nil
}

func newZipReader(bundle []byte) (*zip.Reader, error) {
	readerAt := bytes.NewReader(bundle)
	zipReader, err := zip.NewReader(readerAt, int64(len(bundle)))
	if err != nil {
		return nil, fmt.Errorf("parse bundle zip: %w", err)
	}
	return zipReader, nil
}

func writeBundleArtifacts(files []*zip.File, dir string, opts extractOptions) (int, error) {
	var written int
	for _, file := range files {
		name := file.Name
		kind, prefix, ok := classifyArtifact(name, opts)
		if !ok {
			continue
		}
		if skipArtifactFile(file, kind) {
			continue
		}
		outPath, err := artifactOutputPath(dir, name, prefix, opts.PreserveStructure)
		if err != nil {
			return written, err
		}
		if err := writeBundleFile(outPath, file); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func skipArtifactFile(file *zip.File, kind artifactKind) bool {
	if file.FileInfo().IsDir() {
		return true
	}
	if kind == artifactResource {
		return false
	}
	return !strings.HasSuffix(strings.ToLower(file.Name), ".xml")
}

func artifactOutputPath(dir, name, prefix string, preserveStructure bool) (string, error) {
	rel := buildArtifactRelPath(name, prefix, preserveStructure)
	outPath := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("ensure output dir for %s: %w", outPath, err)
	}
	return outPath, nil
}

func writeBundleFile(outPath string, file *zip.File) error {
	rc, err := file.Open()
	if err != nil {
		return fmt.Errorf("open %s in bundle: %w", file.Name, err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return fmt.Errorf("read %s: %w", file.Name, err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	return nil
}

type artifactKind int

const (
	artifactProxyEndpoint artifactKind = iota
	artifactTargetEndpoint
	artifactPolicy
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
	if prefix, ok := policyPrefix(name); ok {
		if !opts.IncludePolicies {
			return artifactPolicy, "", false
		}
		return artifactPolicy, prefix, true
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

func policyPrefix(name string) (string, bool) {
	if strings.HasPrefix(name, "apiproxy/policies/") {
		return "apiproxy/policies/", true
	}
	if strings.HasPrefix(name, "apiproxy/policy/") {
		return "apiproxy/policy/", true
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
	req.Header.Set("Authorization", bearerPrefix+c.token)
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
	if names, ok := decodeNameListArray(data); ok {
		return names, nil
	}
	if names, ok := decodeNameListContainer(data); ok {
		return names, nil
	}
	if names, ok := decodeNameListKeyed(data); ok {
		return names, nil
	}
	if names, ok := decodeNameListObjects(data); ok {
		return names, nil
	}
	return nil, fmt.Errorf("unsupported response: %s", strings.TrimSpace(string(data)))
}

func decodeNameListArray(data []byte) ([]string, bool) {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, true
	}
	return nil, false
}

func decodeNameListContainer(data []byte) ([]string, bool) {
	var container nameContainer
	if err := json.Unmarshal(data, &container); err != nil {
		return nil, false
	}
	names := collectNames(container.Proxies)
	names = append(names, collectNames(container.Items)...)
	names = append(names, collectNames(container.APIs)...)
	names = append(names, collectNames(container.APIProducts)...)
	names = append(names, collectNames(container.Apps)...)
	names = append(names, collectNames(container.AppsList)...)
	names = append(names, container.AppIDs...)
	if len(names) > 0 {
		return names, true
	}
	return nil, false
}

func decodeNameListKeyed(data []byte) ([]string, bool) {
	var keyed map[string][]string
	if err := json.Unmarshal(data, &keyed); err != nil {
		return nil, false
	}
	for _, key := range []string{"apis", "items", "proxies", "apiProduct", "app", "apps", "appIds", "appids"} {
		if names := keyed[key]; len(names) > 0 {
			return names, true
		}
	}
	return nil, false
}

func decodeNameListObjects(data []byte) ([]string, bool) {
	var objects []map[string]interface{}
	if err := json.Unmarshal(data, &objects); err != nil {
		return nil, false
	}
	var names []string
	for _, obj := range objects {
		if name := extractNameish(obj); name != "" {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		return names, true
	}
	return nil, false
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
	return firstStringValue(obj, nameKeys())
}

type bundleEndpoint struct {
	Name            string
	BasePath        string
	TargetServers   []string
	TargetEndpoints []proxyxml.TargetEndpointDetails
	FlowCount       int
	FlowSteps       proxyxml.FlowSteps
}

type proxyMeta struct {
	basePath     string
	targetRoutes []string
	flowCount    int
	flowSteps    proxyxml.FlowSteps
}

func parseProxyEndpointsFromBundle(bundle []byte) ([]bundleEndpoint, error) {
	readerAt := bytes.NewReader(bundle)
	zipReader, err := zip.NewReader(readerAt, int64(len(bundle)))
	if err != nil {
		return nil, fmt.Errorf("parse bundle zip: %w", err)
	}

	targetEndpoints, proxyData, err := collectBundleData(zipReader.File)
	if err != nil {
		return nil, err
	}
	return buildBundleEndpoints(targetEndpoints, proxyData)
}

func shouldSkipBundleEntry(name string, isDir bool) bool {
	if isDir {
		return true
	}
	return !strings.HasSuffix(strings.ToLower(name), ".xml")
}

func readZipEntry(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s in bundle: %w", file.Name, err)
	}
	data, readErr := io.ReadAll(rc)
	rc.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read %s: %w", file.Name, readErr)
	}
	return data, nil
}

func applyTargetEndpointData(name string, data []byte, targetEndpoints map[string]proxyxml.TargetEndpointDetails) (bool, error) {
	targetPrefix, ok := targetEndpointPrefix(name)
	if !ok {
		return false, nil
	}
	rel := strings.TrimPrefix(name, targetPrefix)
	if rel == "" {
		return true, nil
	}
	details, err := proxyxml.ParseTargetEndpointDetails(data)
	if err != nil {
		return true, fmt.Errorf("parse target endpoint %s: %w", name, err)
	}
	targetName := details.Name
	if targetName == "" {
		targetName = strings.TrimSuffix(rel, path.Ext(rel))
	}
	if targetName == "" {
		return true, nil
	}
	details.Name = targetName
	key := strings.ToLower(targetName)
	if existing, ok := targetEndpoints[key]; ok {
		details.LoadBalancer = util.MergeAndUnique(existing.LoadBalancer, details.LoadBalancer)
		if len(existing.Properties) > 0 && len(details.Properties) == 0 {
			details.Properties = existing.Properties
			details.SuccessCodes = existing.SuccessCodes
		}
		if details.URL == "" {
			details.URL = existing.URL
		}
	}
	targetEndpoints[key] = details
	return true, nil
}

func applyProxyEndpointData(name string, data []byte, proxyData map[string]proxyMeta) (bool, error) {
	prefix, ok := proxyEndpointPrefix(name)
	if !ok {
		return false, nil
	}

	endpointName := extractProxyEndpointName(name, prefix)
	if endpointName == "" {
		return true, nil
	}
	flows, err := proxyxml.ParseFlows(data)
	if err != nil {
		return true, fmt.Errorf("parse flows for %s: %w", name, err)
	}
	flowSteps, err := proxyxml.ParseFlowSteps(data)
	if err != nil {
		return true, fmt.Errorf("parse flow steps for %s: %w", name, err)
	}
	basePath, err := proxyxml.ExtractBasePath(data)
	if err != nil {
		return true, fmt.Errorf("parse base path for %s: %w", name, err)
	}
	targets, err := proxyxml.ExtractRouteTargets(data)
	if err != nil {
		return true, fmt.Errorf("parse route targets for %s: %w", name, err)
	}
	proxyData[endpointName] = proxyMeta{
		basePath:     basePath,
		targetRoutes: targets,
		flowCount:    len(flows),
		flowSteps:    flowSteps,
	}
	return true, nil
}

func extractProxyEndpointName(name, prefix string) string {
	rel := strings.TrimPrefix(name, prefix)
	rel = strings.TrimLeft(rel, "/")
	if rel == "" {
		return ""
	}
	segment := rel
	if idx := strings.Index(segment, "/"); idx >= 0 {
		segment = segment[:idx]
	}
	return strings.TrimSuffix(segment, path.Ext(segment))
}

func nameKeys() []string {
	return []string{"name", "appId", "appID", "app_id", "app_name", "appName", "id"}
}

func firstStringValue(obj map[string]interface{}, keys []string) string {
	for _, key := range keys {
		if v, ok := obj[key]; ok {
			if s, ok := v.(string); ok {
				value := strings.TrimSpace(s)
				if value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func collectBundleData(files []*zip.File) (map[string]proxyxml.TargetEndpointDetails, map[string]proxyMeta, error) {
	targetEndpoints := make(map[string]proxyxml.TargetEndpointDetails)
	proxyData := make(map[string]proxyMeta)
	for _, file := range files {
		if err := processBundleEntry(file, targetEndpoints, proxyData); err != nil {
			return nil, nil, err
		}
	}
	return targetEndpoints, proxyData, nil
}

func processBundleEntry(file *zip.File, targetEndpoints map[string]proxyxml.TargetEndpointDetails, proxyData map[string]proxyMeta) error {
	if shouldSkipBundleEntry(file.Name, file.FileInfo().IsDir()) {
		return nil
	}
	data, err := readZipEntry(file)
	if err != nil {
		return err
	}
	if handled, err := applyTargetEndpointData(file.Name, data, targetEndpoints); handled || err != nil {
		return err
	}
	if handled, err := applyProxyEndpointData(file.Name, data, proxyData); handled || err != nil {
		return err
	}
	return nil
}

func buildBundleEndpoints(targetEndpoints map[string]proxyxml.TargetEndpointDetails, proxyData map[string]proxyMeta) ([]bundleEndpoint, error) {
	if len(proxyData) == 0 {
		return nil, fmt.Errorf("no ProxyEndpoint files found in Apigee bundle")
	}

	endpoints := make([]bundleEndpoint, 0, len(proxyData))
	for name, meta := range proxyData {
		servers := collectTargetServers(meta.targetRoutes, targetEndpoints)
		endpointTargets := collectTargetEndpoints(meta.targetRoutes, targetEndpoints)
		endpoints = append(endpoints, bundleEndpoint{
			Name:            name,
			BasePath:        meta.basePath,
			TargetServers:   util.MergeAndUnique(nil, servers),
			TargetEndpoints: endpointTargets,
			FlowCount:       meta.flowCount,
			FlowSteps:       meta.flowSteps,
		})
	}
	return endpoints, nil
}

func collectTargetServers(targetRoutes []string, targetEndpoints map[string]proxyxml.TargetEndpointDetails) []string {
	var servers []string
	for _, tgt := range targetRoutes {
		key := strings.ToLower(strings.TrimSpace(tgt))
		if key == "" {
			continue
		}
		if details, ok := targetEndpoints[key]; ok {
			servers = append(servers, details.LoadBalancer...)
		}
	}
	return servers
}

func collectTargetEndpoints(targetRoutes []string, targetEndpoints map[string]proxyxml.TargetEndpointDetails) []proxyxml.TargetEndpointDetails {
	seen := make(map[string]struct{})
	var result []proxyxml.TargetEndpointDetails
	for _, tgt := range targetRoutes {
		key := strings.ToLower(strings.TrimSpace(tgt))
		if key == "" {
			continue
		}
		details, ok := targetEndpoints[key]
		if !ok || strings.TrimSpace(details.Name) == "" {
			continue
		}
		nameKey := strings.ToLower(strings.TrimSpace(details.Name))
		if _, exists := seen[nameKey]; exists {
			continue
		}
		seen[nameKey] = struct{}{}
		result = append(result, details)
	}
	return result
}

// FindClosestProxyEndpoint finds the downloaded ProxyEndpoint XML that is most similar to the generated file.
func FindClosestProxyEndpoint(generatedPath, dir string) (string, float64, error) {
	baseSignatures, err := readBaseSignatures(generatedPath)
	if err != nil {
		return "", 0, err
	}
	dir, err = validateDir(dir)
	if err != nil {
		return "", 0, err
	}
	bestFile, bestScore, err := findClosestProxyEndpointInDir(dir, baseSignatures)
	if err != nil {
		return "", 0, err
	}
	if bestFile == "" {
		return "", 0, fmt.Errorf("no XML files found in %s", dir)
	}
	return bestFile, bestScore, nil
}

func readBaseSignatures(generatedPath string) ([]string, error) {
	generatedPath = strings.TrimSpace(generatedPath)
	if generatedPath == "" {
		return nil, fmt.Errorf("generated ProxyEndpoint path is empty")
	}
	baseData, err := os.ReadFile(generatedPath)
	if err != nil {
		return nil, fmt.Errorf("read generated ProxyEndpoint: %w", err)
	}
	baseFlows, err := proxyxml.ParseFlows(baseData)
	if err != nil {
		return nil, fmt.Errorf("parse generated flows: %w", err)
	}
	return flowSignatures(baseFlows), nil
}

func validateDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("stat download dir: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("download path is not a directory: %s", dir)
	}
	return dir, nil
}

func findClosestProxyEndpointInDir(dir string, baseSignatures []string) (string, float64, error) {
	var bestFile string
	var bestScore float64

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(path)) != ".xml" {
			return nil
		}
		score, err := flowSimilarityForFile(path, baseSignatures)
		if err != nil {
			return err
		}
		if score > bestScore {
			bestScore = score
			bestFile = path
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return bestFile, bestScore, nil
}

func flowSimilarityForFile(path string, baseSignatures []string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	flows, err := proxyxml.ParseFlows(data)
	if err != nil {
		return 0, fmt.Errorf("parse flows in %s: %w", path, err)
	}
	return flowSimilarity(baseSignatures, flowSignatures(flows)), nil
}

func flowSignatures(flows []proxyxml.Flow) []string {
	if len(flows) == 0 {
		return nil
	}
	signatures := make([]string, 0, len(flows))
	for _, fl := range flows {
		name := strings.TrimSpace(fl.Name)
		if strings.EqualFold(name, "NotFound") {
			continue
		}
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
