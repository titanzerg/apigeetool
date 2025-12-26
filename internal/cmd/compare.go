package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"apigee/internal/apigee"
	"apigee/internal/proxyxml"
	"apigee/internal/report"
)

type CompareArgs struct {
	ProxyName   string
	RevisionA   int
	RevisionB   int
	DownloadDir string
}

func RunCompare(cfg ApigeeConfig, args CompareArgs) error {
	proxy := strings.TrimSpace(args.ProxyName)
	if proxy == "" {
		return fmt.Errorf("proxy name is required for compare (-proxy)")
	}
	if args.RevisionA <= 0 || args.RevisionB <= 0 {
		return fmt.Errorf("compare requires two positive revision numbers")
	}
	if err := RequireApigeeAuth(cfg, "comparing proxies"); err != nil {
		return err
	}

	baseDir := DefaultString(args.DownloadDir, "downloaded-proxy-endpoints")
	leftDir := filepath.Join(baseDir, fmt.Sprintf("compare-rev-%d", args.RevisionA))
	rightDir := filepath.Join(baseDir, fmt.Sprintf("compare-rev-%d", args.RevisionB))

	if err := os.RemoveAll(leftDir); err != nil {
		return fmt.Errorf("cleanup compare dir %s: %w", leftDir, err)
	}
	if err := os.RemoveAll(rightDir); err != nil {
		return fmt.Errorf("cleanup compare dir %s: %w", rightDir, err)
	}

	if err := apigee.DownloadProxyArtifacts(apigee.DownloadOptions{
		Host:                   cfg.Host,
		Org:                    cfg.Org,
		Proxy:                  proxy,
		Token:                  cfg.Token,
		Revision:               args.RevisionA,
		OutputDir:              leftDir,
		Quiet:                  true,
		IncludeProxyEndpoints:  true,
		IncludeTargetEndpoints: true,
		IncludeResources:       true,
		PreserveStructure:      true,
	}); err != nil {
		return fmt.Errorf("download revision %d: %w", args.RevisionA, err)
	}

	if err := apigee.DownloadProxyArtifacts(apigee.DownloadOptions{
		Host:                   cfg.Host,
		Org:                    cfg.Org,
		Proxy:                  proxy,
		Token:                  cfg.Token,
		Revision:               args.RevisionB,
		OutputDir:              rightDir,
		Quiet:                  true,
		IncludeProxyEndpoints:  true,
		IncludeTargetEndpoints: true,
		IncludeResources:       true,
		PreserveStructure:      true,
	}); err != nil {
		return fmt.Errorf("download revision %d: %w", args.RevisionB, err)
	}

	leftProxyFiles, err := collectFilesByPrefixes(leftDir, proxyEndpointPrefixes(), true)
	if err != nil {
		return err
	}
	rightProxyFiles, err := collectFilesByPrefixes(rightDir, proxyEndpointPrefixes(), true)
	if err != nil {
		return err
	}
	leftTargetFiles, err := collectFilesByPrefixes(leftDir, targetEndpointPrefixes(), true)
	if err != nil {
		return err
	}
	rightTargetFiles, err := collectFilesByPrefixes(rightDir, targetEndpointPrefixes(), true)
	if err != nil {
		return err
	}
	leftResourceFiles, err := collectFilesByPrefixes(leftDir, resourcePrefixes(), false)
	if err != nil {
		return err
	}
	rightResourceFiles, err := collectFilesByPrefixes(rightDir, resourcePrefixes(), false)
	if err != nil {
		return err
	}

	leftLabel := fmt.Sprintf("revision %d", args.RevisionA)
	rightLabel := fmt.Sprintf("revision %d", args.RevisionB)

	diffFound := false

	onlyLeft, onlyRight, common := compareFileSets(leftProxyFiles, rightProxyFiles)
	for _, rel := range onlyLeft {
		fmt.Printf("ProxyEndpoint only in %s: %s\n", leftLabel, rel)
		diffFound = true
	}
	for _, rel := range onlyRight {
		fmt.Printf("ProxyEndpoint only in %s: %s\n", rightLabel, rel)
		diffFound = true
	}

	for _, rel := range common {
		leftPath := leftProxyFiles[rel]
		rightPath := rightProxyFiles[rel]

		leftBase, err := readBasePath(leftPath)
		if err != nil {
			return err
		}
		rightBase, err := readBasePath(rightPath)
		if err != nil {
			return err
		}

		leftFlows, err := proxyxml.ParseFlowsFromFile(leftPath)
		if err != nil {
			return fmt.Errorf("parse flows from %s: %w", leftPath, err)
		}
		rightFlows, err := proxyxml.ParseFlowsFromFile(rightPath)
		if err != nil {
			return fmt.Errorf("parse flows from %s: %w", rightPath, err)
		}

		printedHeader := false
		if leftBase != rightBase {
			fmt.Printf("ProxyEndpoint %s:\n", rel)
			fmt.Printf("BasePath differs:\n  %s: %s\n  %s: %s\n", leftLabel, leftBase, rightLabel, rightBase)
			printedHeader = true
		}

		flowDiff := proxyxml.DiffFlows(leftFlows, rightFlows)
		if len(flowDiff.Missing) > 0 || len(flowDiff.Extra) > 0 || len(flowDiff.Changed) > 0 {
			if !printedHeader {
				fmt.Printf("ProxyEndpoint %s:\n", rel)
				printedHeader = true
			}
			report.PrintFlowDiffWithLabels(flowDiff, leftLabel, rightLabel)
		}

		if printedHeader {
			fmt.Println()
			diffFound = true
		}
	}

	targetDiff, err := compareTextArtifacts(leftTargetFiles, rightTargetFiles, "TargetEndpoint", leftLabel, rightLabel, true)
	if err != nil {
		return err
	}
	if targetDiff {
		diffFound = true
	}

	resourceDiff, err := compareResourceArtifacts(leftResourceFiles, rightResourceFiles, leftLabel, rightLabel)
	if err != nil {
		return err
	}
	if resourceDiff {
		diffFound = true
	}

	if !diffFound {
		fmt.Println("No differences detected.")
	}
	return nil
}

func collectFilesByPrefixes(dir string, prefixes []string, xmlOnly bool) (map[string]string, error) {
	files := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if xmlOnly && !strings.HasSuffix(strings.ToLower(rel), ".xml") {
			return nil
		}
		if !hasPrefix(rel, prefixes) {
			return nil
		}
		files[rel] = path
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	return files, nil
}

func compareFileSets(left, right map[string]string) ([]string, []string, []string) {
	var onlyLeft []string
	for rel := range left {
		if _, ok := right[rel]; !ok {
			onlyLeft = append(onlyLeft, rel)
		}
	}
	var onlyRight []string
	for rel := range right {
		if _, ok := left[rel]; !ok {
			onlyRight = append(onlyRight, rel)
		}
	}
	var common []string
	for rel := range left {
		if _, ok := right[rel]; ok {
			common = append(common, rel)
		}
	}
	sort.Strings(onlyLeft)
	sort.Strings(onlyRight)
	sort.Strings(common)
	return onlyLeft, onlyRight, common
}

func readBasePath(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	basePath, err := proxyxml.ExtractBasePath(data)
	if err != nil {
		return "", fmt.Errorf("parse BasePath from %s: %w", path, err)
	}
	return basePath, nil
}

func compareTextArtifacts(left, right map[string]string, label, leftSide, rightSide string, showUnifiedDiff bool) (bool, error) {
	onlyLeft, onlyRight, common := compareFileSets(left, right)
	diffFound := false
	for _, rel := range onlyLeft {
		fmt.Printf("%s only in %s: %s\n", label, leftSide, rel)
		diffFound = true
	}
	for _, rel := range onlyRight {
		fmt.Printf("%s only in %s: %s\n", label, rightSide, rel)
		diffFound = true
	}
	for _, rel := range common {
		leftPath := left[rel]
		rightPath := right[rel]
		leftData, err := os.ReadFile(leftPath)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", leftPath, err)
		}
		rightData, err := os.ReadFile(rightPath)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", rightPath, err)
		}
		if normalizeWhitespace(string(leftData)) != normalizeWhitespace(string(rightData)) {
			if showUnifiedDiff {
				printUnifiedDiff(label, rel, leftPath, rightPath)
			} else {
				fmt.Printf("%s differs: %s\n", label, rel)
			}
			diffFound = true
		}
	}
	return diffFound, nil
}

func compareResourceArtifacts(left, right map[string]string, leftSide, rightSide string) (bool, error) {
	onlyLeft, onlyRight, common := compareFileSets(left, right)
	diffFound := false
	for _, rel := range onlyLeft {
		fmt.Printf("Resource only in %s: %s\n", leftSide, rel)
		diffFound = true
	}
	for _, rel := range onlyRight {
		fmt.Printf("Resource only in %s: %s\n", rightSide, rel)
		diffFound = true
	}
	for _, rel := range common {
		leftPath := left[rel]
		rightPath := right[rel]
		leftData, err := os.ReadFile(leftPath)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", leftPath, err)
		}
		rightData, err := os.ReadFile(rightPath)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", rightPath, err)
		}
		if !bytes.Equal(leftData, rightData) {
			fmt.Printf("Resource differs: %s\n", rel)
			diffFound = true
		}
	}
	return diffFound, nil
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func printUnifiedDiff(label, rel, leftPath, rightPath string) {
	diffPath, err := exec.LookPath("diff")
	if err != nil {
		fmt.Printf("%s differs: %s\n", label, rel)
		return
	}
	cmd := exec.Command(diffPath, "-u", leftPath, rightPath)
	output, err := cmd.CombinedOutput()
	if len(output) == 0 {
		fmt.Printf("%s differs: %s\n", label, rel)
		return
	}
	fmt.Printf("Unified diff (%s %s):\n", label, rel)
	fmt.Print(string(output))
	if err != nil {
		// diff returns exit code 1 when differences exist, which is expected.
		return
	}
}

func hasPrefix(rel string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

func proxyEndpointPrefixes() []string {
	return []string{
		"proxies/",
		"proxy-endpoints/",
		"proxy_endpoints/",
		"proxy-endpoint/",
		"proxy_endpoint/",
	}
}

func targetEndpointPrefixes() []string {
	return []string{
		"targets/",
		"target-endpoints/",
		"target_endpoints/",
		"target-endpoint/",
		"target_endpoint/",
	}
}

func resourcePrefixes() []string {
	return []string{
		"resources/",
	}
}
