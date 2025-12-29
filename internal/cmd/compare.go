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

const (
	errReadFileFmt   = "read %s: %w"
	differsOutputFmt = "%s differs: %s\n"
	onlyInSideFmt    = "%s only in %s: %s\n"
)

type CompareArgs struct {
	ProxyName   string
	RevisionA   int
	RevisionB   int
	DownloadDir string
}

func RunCompare(cfg ApigeeConfig, args CompareArgs) error {
	proxy, err := validateCompareArgs(cfg, args)
	if err != nil {
		return err
	}
	leftDir, rightDir := compareDirs(args)
	if err := prepareCompareDirs(leftDir, rightDir); err != nil {
		return err
	}
	if err := downloadCompareRevisions(cfg, proxy, args, leftDir, rightDir); err != nil {
		return err
	}
	fileSets, labels, err := buildCompareContext(leftDir, rightDir, args)
	if err != nil {
		return err
	}

	diffFound, err := compareProxyEndpointSets(fileSets, labels)
	if err != nil {
		return err
	}
	diffFound, err = compareTargetsAndResources(fileSets, labels, diffFound)
	if err != nil {
		return err
	}
	if !diffFound {
		fmt.Println("No differences detected.")
	}
	return nil
}

type compareLabels struct {
	Left  string
	Right string
}

type compareArtifactSets struct {
	LeftProxy     map[string]string
	RightProxy    map[string]string
	LeftTarget    map[string]string
	RightTarget   map[string]string
	LeftResource  map[string]string
	RightResource map[string]string
}

func validateCompareArgs(cfg ApigeeConfig, args CompareArgs) (string, error) {
	proxy := strings.TrimSpace(args.ProxyName)
	if proxy == "" {
		return "", fmt.Errorf("proxy name is required for compare (-proxy)")
	}
	if args.RevisionA <= 0 || args.RevisionB <= 0 {
		return "", fmt.Errorf("compare requires two positive revision numbers")
	}
	if err := RequireApigeeAuth(cfg, "comparing proxies"); err != nil {
		return "", err
	}
	return proxy, nil
}

func compareDirs(args CompareArgs) (string, string) {
	baseDir := DefaultString(args.DownloadDir, "downloaded-proxy-endpoints")
	leftDir := filepath.Join(baseDir, fmt.Sprintf("compare-rev-%d", args.RevisionA))
	rightDir := filepath.Join(baseDir, fmt.Sprintf("compare-rev-%d", args.RevisionB))
	return leftDir, rightDir
}

func prepareCompareDirs(leftDir, rightDir string) error {
	if err := os.RemoveAll(leftDir); err != nil {
		return fmt.Errorf("cleanup compare dir %s: %w", leftDir, err)
	}
	if err := os.RemoveAll(rightDir); err != nil {
		return fmt.Errorf("cleanup compare dir %s: %w", rightDir, err)
	}
	return nil
}

func downloadCompareRevisions(cfg ApigeeConfig, proxy string, args CompareArgs, leftDir, rightDir string) error {
	if err := downloadCompareRevision(cfg, proxy, args.RevisionA, leftDir); err != nil {
		return err
	}
	if err := downloadCompareRevision(cfg, proxy, args.RevisionB, rightDir); err != nil {
		return err
	}
	return nil
}

func downloadCompareRevision(cfg ApigeeConfig, proxy string, revision int, outputDir string) error {
	if err := apigee.DownloadProxyArtifacts(apigee.DownloadOptions{
		Host:                   cfg.Host,
		Org:                    cfg.Org,
		Proxy:                  proxy,
		Token:                  cfg.Token,
		Revision:               revision,
		OutputDir:              outputDir,
		Quiet:                  true,
		IncludeProxyEndpoints:  true,
		IncludeTargetEndpoints: true,
		IncludeResources:       true,
		PreserveStructure:      true,
	}); err != nil {
		return fmt.Errorf("download revision %d: %w", revision, err)
	}
	return nil
}

func buildCompareContext(leftDir, rightDir string, args CompareArgs) (compareArtifactSets, compareLabels, error) {
	leftProxyFiles, err := collectFilesByPrefixes(leftDir, proxyEndpointPrefixes(), true)
	if err != nil {
		return compareArtifactSets{}, compareLabels{}, err
	}
	rightProxyFiles, err := collectFilesByPrefixes(rightDir, proxyEndpointPrefixes(), true)
	if err != nil {
		return compareArtifactSets{}, compareLabels{}, err
	}
	leftTargetFiles, err := collectFilesByPrefixes(leftDir, targetEndpointPrefixes(), true)
	if err != nil {
		return compareArtifactSets{}, compareLabels{}, err
	}
	rightTargetFiles, err := collectFilesByPrefixes(rightDir, targetEndpointPrefixes(), true)
	if err != nil {
		return compareArtifactSets{}, compareLabels{}, err
	}
	leftResourceFiles, err := collectFilesByPrefixes(leftDir, resourcePrefixes(), false)
	if err != nil {
		return compareArtifactSets{}, compareLabels{}, err
	}
	rightResourceFiles, err := collectFilesByPrefixes(rightDir, resourcePrefixes(), false)
	if err != nil {
		return compareArtifactSets{}, compareLabels{}, err
	}

	labels := compareLabels{
		Left:  fmt.Sprintf("revision %d", args.RevisionA),
		Right: fmt.Sprintf("revision %d", args.RevisionB),
	}
	fileSets := compareArtifactSets{
		LeftProxy:     leftProxyFiles,
		RightProxy:    rightProxyFiles,
		LeftTarget:    leftTargetFiles,
		RightTarget:   rightTargetFiles,
		LeftResource:  leftResourceFiles,
		RightResource: rightResourceFiles,
	}
	return fileSets, labels, nil
}

func compareProxyEndpointSets(files compareArtifactSets, labels compareLabels) (bool, error) {
	diffFound := false
	onlyLeft, onlyRight, common := compareFileSets(files.LeftProxy, files.RightProxy)
	diffFound = markOnlyInSide("ProxyEndpoint", labels, onlyLeft, onlyRight) || diffFound

	for _, rel := range common {
		diff, err := compareProxyEndpointFile(rel, files.LeftProxy[rel], files.RightProxy[rel], labels)
		if err != nil {
			return false, err
		}
		if diff {
			diffFound = true
		}
	}
	return diffFound, nil
}

func compareTargetsAndResources(files compareArtifactSets, labels compareLabels, diffFound bool) (bool, error) {
	targetDiff, err := compareTextArtifacts(files.LeftTarget, files.RightTarget, "TargetEndpoint", labels.Left, labels.Right, true)
	if err != nil {
		return diffFound, err
	}
	resourceDiff, err := compareResourceArtifacts(files.LeftResource, files.RightResource, labels.Left, labels.Right)
	if err != nil {
		return diffFound, err
	}
	return diffFound || targetDiff || resourceDiff, nil
}

func markOnlyInSide(label string, labels compareLabels, onlyLeft, onlyRight []string) bool {
	diffFound := false
	for _, rel := range onlyLeft {
		fmt.Printf(onlyInSideFmt, label, labels.Left, rel)
		diffFound = true
	}
	for _, rel := range onlyRight {
		fmt.Printf(onlyInSideFmt, label, labels.Right, rel)
		diffFound = true
	}
	return diffFound
}

func compareProxyEndpointFile(rel, leftPath, rightPath string, labels compareLabels) (bool, error) {
	leftBase, err := readBasePath(leftPath)
	if err != nil {
		return false, err
	}
	rightBase, err := readBasePath(rightPath)
	if err != nil {
		return false, err
	}

	leftFlows, err := proxyxml.ParseFlowsFromFile(leftPath)
	if err != nil {
		return false, fmt.Errorf("parse flows from %s: %w", leftPath, err)
	}
	rightFlows, err := proxyxml.ParseFlowsFromFile(rightPath)
	if err != nil {
		return false, fmt.Errorf("parse flows from %s: %w", rightPath, err)
	}

	printedHeader := false
	if leftBase != rightBase {
		fmt.Printf("ProxyEndpoint %s:\n", rel)
		fmt.Printf("BasePath differs:\n  %s: %s\n  %s: %s\n", labels.Left, leftBase, labels.Right, rightBase)
		printedHeader = true
	}

	flowDiff := proxyxml.DiffFlows(leftFlows, rightFlows)
	if len(flowDiff.Missing) > 0 || len(flowDiff.Extra) > 0 || len(flowDiff.Changed) > 0 {
		if !printedHeader {
			fmt.Printf("ProxyEndpoint %s:\n", rel)
			printedHeader = true
		}
		report.PrintFlowDiffWithLabels(flowDiff, labels.Left, labels.Right)
	}

	if printedHeader {
		fmt.Println()
		return true, nil
	}
	return false, nil
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
		return "", fmt.Errorf(errReadFileFmt, path, err)
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
		fmt.Printf(onlyInSideFmt, label, leftSide, rel)
		diffFound = true
	}
	for _, rel := range onlyRight {
		fmt.Printf(onlyInSideFmt, label, rightSide, rel)
		diffFound = true
	}
	for _, rel := range common {
		leftPath := left[rel]
		rightPath := right[rel]
		leftData, err := os.ReadFile(leftPath)
		if err != nil {
			return false, fmt.Errorf(errReadFileFmt, leftPath, err)
		}
		rightData, err := os.ReadFile(rightPath)
		if err != nil {
			return false, fmt.Errorf(errReadFileFmt, rightPath, err)
		}
		if normalizeWhitespace(string(leftData)) != normalizeWhitespace(string(rightData)) {
			if showUnifiedDiff {
				printUnifiedDiff(label, rel, leftPath, rightPath)
			} else {
				fmt.Printf(differsOutputFmt, label, rel)
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
		fmt.Printf(onlyInSideFmt, "Resource", leftSide, rel)
		diffFound = true
	}
	for _, rel := range onlyRight {
		fmt.Printf(onlyInSideFmt, "Resource", rightSide, rel)
		diffFound = true
	}
	for _, rel := range common {
		leftPath := left[rel]
		rightPath := right[rel]
		leftData, err := os.ReadFile(leftPath)
		if err != nil {
			return false, fmt.Errorf(errReadFileFmt, leftPath, err)
		}
		rightData, err := os.ReadFile(rightPath)
		if err != nil {
			return false, fmt.Errorf(errReadFileFmt, rightPath, err)
		}
		if !bytes.Equal(leftData, rightData) {
			printUnifiedDiff("Resource", rel, leftPath, rightPath)
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
		fmt.Printf(differsOutputFmt, label, rel)
		return
	}
	cmd := exec.Command(diffPath, "-u", leftPath, rightPath)
	output, err := cmd.CombinedOutput()
	if len(output) == 0 {
		fmt.Printf(differsOutputFmt, label, rel)
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
