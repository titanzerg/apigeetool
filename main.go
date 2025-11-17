package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
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
		inputPath  = flag.String("input", "openapi.yaml", "path to the OpenAPI v3 file")
		outputPath = flag.String("output", "proxy-endpoint.xml", "output path for the Apigee ProxyEndpoint XML")
		name       = flag.String("name", "default", "ProxyEndpoint name attribute")
		basePath   = flag.String("basepath", "", "HTTPProxyConnection BasePath (defaults to slugified info.title)")
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
