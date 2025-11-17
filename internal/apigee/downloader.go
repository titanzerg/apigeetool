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
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

	host := strings.TrimSuffix(strings.TrimSpace(opts.Host), "/")
	if host == "" {
		host = "https://apigee.googleapis.com"
	}

	client := &Client{
		host:       host,
		org:        org,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

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
		score := similarityScore(string(baseData), string(data))
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

func similarityScore(a, b string) float64 {
	linesA := normalizedLines(a)
	linesB := normalizedLines(b)
	if len(linesA) == 0 && len(linesB) == 0 {
		return 1
	}

	countA := make(map[string]int, len(linesA))
	for _, line := range linesA {
		countA[line]++
	}

	countB := make(map[string]int, len(linesB))
	for _, line := range linesB {
		countB[line]++
	}

	var matches int
	for line, aCount := range countA {
		if bCount := countB[line]; bCount > 0 {
			if aCount < bCount {
				matches += aCount
			} else {
				matches += bCount
			}
		}
	}

	total := len(linesA) + len(linesB)
	if total == 0 {
		return 0
	}
	return float64(2*matches) / float64(total)
}

func normalizedLines(content string) []string {
	raw := strings.Split(content, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
