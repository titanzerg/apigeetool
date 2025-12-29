package update

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

const indentUnit = "  "

// ConfirmApply prompts the user to confirm whether the downloaded ProxyEndpoint should be updated.
func ConfirmApply() (bool, error) {
	return confirmApply(os.Stdin, os.Stdout)
}

// ReplaceProxyEndpoint updates only the <Flows> section of the target file with the generated contents.
func ReplaceProxyEndpoint(generatedPath, targetPath string) error {
	genData, targetData, err := readProxyEndpointFiles(generatedPath, targetPath)
	if err != nil {
		return err
	}
	targetSegment, err := requireFlowsSegments(genData, targetData)
	if err != nil {
		return err
	}
	generatedFlows, targetFlows, err := parseFlowDetailsPair(genData, targetData)
	if err != nil {
		return err
	}
	merged := mergeFlows(generatedFlows, targetFlows)
	updated := replaceFlowsSegment(targetData, targetSegment, merged)

	formatted, err := formatXML(updated)
	if err != nil {
		return fmt.Errorf("format merged ProxyEndpoint: %w", err)
	}

	return os.WriteFile(targetPath, formatted, 0o644)
}

func confirmApply(in io.Reader, out io.Writer) (bool, error) {
	reader := bufio.NewReader(in)
	for {
		if _, err := fmt.Fprint(out, "Apply generated changes to the downloaded ProxyEndpoint? [y/N]: "); err != nil {
			return false, err
		}
		resp, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		resp = strings.TrimSpace(strings.ToLower(resp))
		if resp == "" {
			return false, nil
		}
		if resp == "y" || resp == "yes" {
			return true, nil
		}
		if resp == "n" || resp == "no" {
			return false, nil
		}
		if _, err := fmt.Fprintln(out, "Please answer y or n."); err != nil {
			return false, err
		}
	}
}

type flowsSegment struct {
	Start    int
	End      int
	OpenTag  []byte
	CloseTag []byte
	Inner    []byte
}

func locateFlowsSegment(data []byte) (flowsSegment, error) {
	start := bytes.Index(data, []byte("<Flows"))
	if start < 0 {
		return flowsSegment{}, fmt.Errorf("<Flows> start tag not found")
	}

	openEndRel := bytes.IndexByte(data[start:], '>')
	if openEndRel < 0 {
		return flowsSegment{}, fmt.Errorf("<Flows> tag is not closed")
	}
	openEnd := start + openEndRel + 1

	closeTag := []byte("</Flows>")
	closeRel := bytes.Index(data[openEnd:], closeTag)
	if closeRel < 0 {
		return flowsSegment{}, fmt.Errorf("</Flows> closing tag not found")
	}
	closeStart := openEnd + closeRel
	closeEnd := closeStart + len(closeTag)

	return flowsSegment{
		Start:    start,
		End:      closeEnd,
		OpenTag:  append([]byte(nil), data[start:openEnd]...),
		CloseTag: append([]byte(nil), data[closeStart:closeEnd]...),
		Inner:    append([]byte(nil), data[openEnd:closeStart]...),
	}, nil
}
