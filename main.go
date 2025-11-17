package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// OpenAPISpec captures the subset of the OpenAPI document we care about.
type OpenAPISpec struct {
	Info  InfoItem            `yaml:"info"`
	Paths map[string]PathItem `yaml:"paths"`
}

type InfoItem struct {
	Title string `yaml:"title"`
}

// PathItem mirrors the OpenAPI Path Item object.
type PathItem struct {
	Delete  *Operation `yaml:"delete"`
	Get     *Operation `yaml:"get"`
	Head    *Operation `yaml:"head"`
	Options *Operation `yaml:"options"`
	Patch   *Operation `yaml:"patch"`
	Post    *Operation `yaml:"post"`
	Put     *Operation `yaml:"put"`
	Trace   *Operation `yaml:"trace"`
}

// Operation captures the few operation fields we need from OpenAPI.
type Operation struct {
	OperationID string `yaml:"operationId"`
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
}

type flow struct {
	Path    string
	Method  string
	Summary string
	Name    string
}

type orderedPath struct {
	Path    string
	Methods []string
}

var methodExtractors = []struct {
	name    string
	extract func(*PathItem) *Operation
}{
	{name: "GET", extract: func(p *PathItem) *Operation { return p.Get }},
	{name: "POST", extract: func(p *PathItem) *Operation { return p.Post }},
	{name: "PUT", extract: func(p *PathItem) *Operation { return p.Put }},
	{name: "DELETE", extract: func(p *PathItem) *Operation { return p.Delete }},
	{name: "PATCH", extract: func(p *PathItem) *Operation { return p.Patch }},
	{name: "OPTIONS", extract: func(p *PathItem) *Operation { return p.Options }},
	{name: "HEAD", extract: func(p *PathItem) *Operation { return p.Head }},
	{name: "TRACE", extract: func(p *PathItem) *Operation { return p.Trace }},
}

func main() {
	var (
		inputPath   = flag.String("input", "openapi.yaml", "path to the OpenAPI v3 file")
		outputPath  = flag.String("output", "proxy-endpoint.xml", "output path for the Apigee ProxyEndpoint XML")
		name        = flag.String("name", "default", "ProxyEndpoint name attribute")
		basePath    = flag.String("basepath", "", "HTTPProxyConnection BasePath (defaults to slugified info.title)")
		proxyName   = flag.String("proxy", "", "Apigee API proxy name to download ProxyEndpoint XML files")
		apigeeOrg   = flag.String("org", "", "Apigee organization to use with -proxy (defaults to APIGEE_ORG)")
		revision    = flag.Int("revision", 0, "Specific revision to download from Apigee (defaults to latest)")
		apigeeHost  = flag.String("apigee-host", "https://apigee.googleapis.com", "Apigee management API base URL")
		downloadDir = flag.String(
			"download-dir",
			"downloaded-proxy-endpoints",
			"Destination directory for downloaded ProxyEndpoint XML files",
		)
		apigeeToken = flag.String("token", "", "Apigee OAuth token (defaults to APIGEE_TOKEN env var)")
	)

	flag.Parse()

	data, err := os.ReadFile(*inputPath)
	if err != nil {
		log.Fatalf("read OpenAPI document: %v", err)
	}

	var spec OpenAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		log.Fatalf("parse OpenAPI YAML: %v", err)
	}

	if len(spec.Paths) == 0 {
		log.Fatal("no paths defined in OpenAPI document")
	}

	ordering := extractPathOrdering(data)
	flows := buildFlows(spec.Paths, ordering)
	if len(flows) == 0 {
		log.Fatal("no operations found under OpenAPI paths")
	}

	path := *basePath
	if strings.TrimSpace(path) == "" {
		path = slugifyTitle(spec.Info.Title)
	}

	xml := renderProxyEndpoint(*name, path, flows)

	if err := os.WriteFile(*outputPath, []byte(xml), 0o644); err != nil {
		log.Fatalf("write output XML: %v", err)
	}

	fmt.Printf("Generated %d flows at %s\n", len(flows), *outputPath)

	if proxy := strings.TrimSpace(*proxyName); proxy != "" {
		org := strings.TrimSpace(*apigeeOrg)
		if org == "" {
			org = strings.TrimSpace(os.Getenv("APIGEE_ORG"))
		}

		token := strings.TrimSpace(*apigeeToken)
		if token == "" {
			token = strings.TrimSpace(os.Getenv("APIGEE_TOKEN"))
		}

		host := strings.TrimSpace(*apigeeHost)
		if host == "" {
			host = "https://apigee.googleapis.com"
		}

		opts := apigeeDownloadOptions{
			Host:      host,
			Org:       org,
			Proxy:     proxy,
			Token:     token,
			Revision:  *revision,
			OutputDir: strings.TrimSpace(*downloadDir),
		}

		if err := downloadProxyEndpoints(opts); err != nil {
			log.Fatalf("download ProxyEndpoint XML: %v", err)
		}
	}
}

func buildFlows(paths map[string]PathItem, ordering []orderedPath) []flow {
	result := make([]flow, 0, len(paths)*2)
	seenPaths := make(map[string]struct{}, len(ordering))

	for _, entry := range ordering {
		item, ok := paths[entry.Path]
		if !ok {
			continue
		}
		result = append(result, flowsForPath(entry.Path, item, entry.Methods)...)
		seenPaths[entry.Path] = struct{}{}
	}

	var leftover []string
	for path := range paths {
		if _, ok := seenPaths[path]; ok {
			continue
		}
		leftover = append(leftover, path)
	}
	sort.Strings(leftover)

	for _, path := range leftover {
		item := paths[path]
		result = append(result, flowsForPath(path, item, nil)...)
	}

	return result
}

func fallbackSummary(op *Operation, method, path string) string {
	if s := strings.TrimSpace(op.Summary); s != "" {
		return s
	}
	if s := strings.TrimSpace(op.Description); s != "" {
		return s
	}
	return fmt.Sprintf("%s %s", titleCase(method), humanizePath(path))
}

func renderProxyEndpoint(name, basePath string, flows []flow) string {
	var buf strings.Builder
	buf.Grow(len(flows) * 256)

	buf.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	buf.WriteByte('\n')
	buf.WriteString(`<ProxyEndpoint name="`)
	buf.WriteString(escapeAttr(name))
	buf.WriteString(`">`)
	buf.WriteByte('\n')
	buf.WriteString("  <Description/>\n")
	buf.WriteString("  <FaultRules/>\n")
	buf.WriteString("  <PreFlow name=\"PreFlow\">\n")
	buf.WriteString("    <Request/>\n")
	buf.WriteString("    <Response/>\n")
	buf.WriteString("  </PreFlow>\n")
	buf.WriteString("  <PostFlow name=\"PostFlow\">\n")
	buf.WriteString("    <Request/>\n")
	buf.WriteString("    <Response/>\n")
	buf.WriteString("  </PostFlow>\n")
	buf.WriteString("  <Flows>\n")

	for _, fl := range flows {
		buf.WriteString("    <Flow name=\"")
		buf.WriteString(escapeAttr(fl.Name))
		buf.WriteString("\">\n")
		buf.WriteString("      <Description>")
		buf.WriteString(escapeText(fl.Summary))
		buf.WriteString("</Description>\n")
		buf.WriteString("      <Request/>\n")
		buf.WriteString("      <Response/>\n")
		buf.WriteString("      <Condition>")
		condition := fmt.Sprintf(`(proxy.pathsuffix MatchesPath "%s") and (request.verb = "%s")`, fl.Path, fl.Method)
		buf.WriteString(escapeText(condition))
		buf.WriteString("</Condition>\n")
		buf.WriteString("    </Flow>\n")
	}

	buf.WriteString("  </Flows>\n")
	buf.WriteString("  <HTTPProxyConnection>\n")
	buf.WriteString("    <BasePath>")
	buf.WriteString(escapeText(basePath))
	buf.WriteString("</BasePath>\n")
	buf.WriteString("    <Properties/>\n")
	buf.WriteString("  </HTTPProxyConnection>\n")
	buf.WriteString("  <RouteRule name=\"default\"/>\n")
	buf.WriteString("</ProxyEndpoint>\n")
	return buf.String()
}

func flowsForPath(path string, item PathItem, orderedMethods []string) []flow {
	var flows []flow
	seenMethods := make(map[string]struct{}, len(orderedMethods))

	appendFlow := func(method string) {
		upper := strings.ToUpper(method)
		if _, exists := seenMethods[upper]; exists {
			return
		}
		op := operationForMethod(&item, upper)
		if op == nil {
			return
		}
		seenMethods[upper] = struct{}{}
		flows = append(flows, flow{
			Path:    path,
			Method:  upper,
			Summary: fallbackSummary(op, upper, path),
			Name:    computeFlowName(op.OperationID, path, upper),
		})
	}

	for _, meta := range methodExtractors {
		appendFlow(meta.name)
	}

	for _, method := range orderedMethods {
		appendFlow(method)
	}

	return flows
}

func escapeText(s string) string {
	var buf bytes.Buffer
	if err := xmlEscape(&buf, s); err != nil {
		// xmlEscape only fails on write errors; we panic to highlight unexpected issues.
		panic(err)
	}
	return buf.String()
}

func escapeAttr(s string) string {
	var buf strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&apos;")
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func xmlEscape(buf *bytes.Buffer, s string) error {
	for _, r := range s {
		switch r {
		case '&':
			_, err := buf.WriteString("&amp;")
			if err != nil {
				return err
			}
		case '<':
			_, err := buf.WriteString("&lt;")
			if err != nil {
				return err
			}
		case '>':
			_, err := buf.WriteString("&gt;")
			if err != nil {
				return err
			}
		default:
			if _, err := buf.WriteRune(r); err != nil {
				return err
			}
		}
	}
	return nil
}

func slugifyTitle(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return "/api"
	}

	var out strings.Builder
	out.Grow(len(title))

	prevDash := false
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			prevDash = false
			continue
		}

		if !prevDash && out.Len() > 0 {
			out.WriteByte('-')
			prevDash = true
		}
	}

	slug := strings.Trim(out.String(), "-")
	if slug == "" {
		return "/api"
	}
	return "/" + slug
}

func humanizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "Resource"
	}

	segments := strings.Split(path, "/")
	var words []string

	for _, seg := range segments {
		seg = strings.Trim(seg, "{}")
		if seg == "" {
			continue
		}
		seg = strings.ReplaceAll(seg, "-", " ")
		words = append(words, seg)
	}

	if len(words) == 0 {
		return "Resource"
	}

	for i, w := range words {
		words[i] = titleCase(w)
	}
	return strings.Join(words, " ")
}

func computeFlowName(operationID, path, method string) string {
	if id := strings.TrimSpace(operationID); id != "" {
		return id
	}
	return fmt.Sprintf("%s %s", path, strings.ToLower(method))
}

func operationForMethod(item *PathItem, method string) *Operation {
	for _, meta := range methodExtractors {
		if meta.name == method {
			return meta.extract(item)
		}
	}
	return nil
}

func extractPathOrdering(data []byte) []orderedPath {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	if len(root.Content) == 0 {
		return nil
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(doc.Content); i += 2 {
		key := doc.Content[i]
		if key.Value != "paths" {
			continue
		}
		value := doc.Content[i+1]
		return parsePathsOrdering(value)
	}

	return nil
}

func parsePathsOrdering(node *yaml.Node) []orderedPath {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	result := make([]orderedPath, 0, len(node.Content)/2)

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		methods := extractMethodOrdering(valueNode)
		result = append(result, orderedPath{
			Path:    keyNode.Value,
			Methods: methods,
		})
	}
	return result
}

func extractMethodOrdering(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	methods := make([]string, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(keyNode.Value))
		if isHTTPMethod(method) {
			methods = append(methods, method)
		}
	}
	return methods
}

func isHTTPMethod(method string) bool {
	for _, meta := range methodExtractors {
		if meta.name == method {
			return true
		}
	}
	return false
}

func titleCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	for i, p := range parts {
		runes := []rune(strings.ToLower(p))
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
		}
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

type apigeeDownloadOptions struct {
	Host      string
	Org       string
	Proxy     string
	Token     string
	Revision  int
	OutputDir string
}

func downloadProxyEndpoints(opts apigeeDownloadOptions) error {
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

	client := &apigeeClient{
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

type apigeeClient struct {
	host       string
	org        string
	token      string
	httpClient *http.Client
}

func (c *apigeeClient) latestRevision(proxy string) (int, error) {
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

func (c *apigeeClient) fetchProxyBundle(proxy string, revision int) ([]byte, error) {
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

func (c *apigeeClient) doRequest(endpoint string) (*http.Response, error) {
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
