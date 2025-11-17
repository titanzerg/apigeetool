package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"Apigee/internal/apigee"
	"Apigee/internal/openapi"
)

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

	spec, err := openapi.Parse(data)
	if err != nil {
		log.Fatalf("parse OpenAPI YAML: %v", err)
	}

	if len(spec.Paths) == 0 {
		log.Fatal("no paths defined in OpenAPI document")
	}

	ordering := openapi.ExtractPathOrdering(data)
	flows := openapi.BuildFlows(spec.Paths, ordering)
	if len(flows) == 0 {
		log.Fatal("no operations found under OpenAPI paths")
	}

	path := strings.TrimSpace(*basePath)
	if path == "" {
		path = openapi.SlugifyTitle(spec.Info.Title)
	}

	xml := openapi.RenderProxyEndpoint(*name, path, flows)

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

		opts := apigee.DownloadOptions{
			Host:      host,
			Org:       org,
			Proxy:     proxy,
			Token:     token,
			Revision:  *revision,
			OutputDir: strings.TrimSpace(*downloadDir),
		}

		if err := apigee.DownloadProxyEndpoints(opts); err != nil {
			log.Fatalf("download ProxyEndpoint XML: %v", err)
		}
	}
}
