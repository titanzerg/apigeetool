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

func readProxyEndpointFiles(generatedPath, targetPath string) ([]byte, []byte, error) {
	genData, err := os.ReadFile(generatedPath)
	if err != nil {
		return nil, nil, err
	}
	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, nil, err
	}
	return genData, targetData, nil
}

func requireFlowsSegments(genData, targetData []byte) (flowsSegment, error) {
	if _, err := locateFlowsSegment(genData); err != nil {
		return flowsSegment{}, fmt.Errorf("generated ProxyEndpoint missing <Flows>: %w", err)
	}
	targetSegment, err := locateFlowsSegment(targetData)
	if err != nil {
		return flowsSegment{}, fmt.Errorf("target ProxyEndpoint missing <Flows>: %w", err)
	}
	return targetSegment, nil
}

func parseFlowDetailsPair(genData, targetData []byte) ([]flowDetails, []flowDetails, error) {
	generatedFlows, err := parseFlowDetails(genData)
	if err != nil {
		return nil, nil, fmt.Errorf("parse generated ProxyEndpoint flows: %w", err)
	}
	targetFlows, err := parseFlowDetails(targetData)
	if err != nil {
		return nil, nil, fmt.Errorf("parse target ProxyEndpoint flows: %w", err)
	}
	return generatedFlows, targetFlows, nil
}

func mergeFlows(generatedFlows, targetFlows []flowDetails) []byte {
	targetFlowMap := make(map[string]flowDetails, len(targetFlows))
	for _, fl := range targetFlows {
		targetFlowMap[fl.Name] = fl
	}

	reqTemplate, respTemplate := mostCommonReqResp(targetFlows)
	if !reqTemplate.Present {
		reqTemplate = newEmptyElement("Request")
	}
	if !respTemplate.Present {
		respTemplate = newEmptyElement("Response")
	}

	generatedNames := make(map[string]struct{}, len(generatedFlows))
	var merged bytes.Buffer

	for _, fl := range generatedFlows {
		generatedNames[fl.Name] = struct{}{}
		req := reqTemplate
		resp := respTemplate
		if existing, ok := targetFlowMap[fl.Name]; ok {
			if existing.Request.Present {
				req = existing.Request
			}
			if existing.Response.Present {
				resp = existing.Response
			}
		}
		merged.WriteString(renderFlow(fl.Name, fl.Description, fl.Condition, req, resp))
	}

	if _, exists := generatedNames["NotFound"]; !exists {
		if nf, ok := targetFlowMap["NotFound"]; ok {
			merged.WriteString(renderFlow(nf.Name, nf.Description, nf.Condition, nf.Request, nf.Response))
		}
	}

	return merged.Bytes()
}

func replaceFlowsSegment(targetData []byte, targetSegment flowsSegment, merged []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(targetData) - (targetSegment.End - targetSegment.Start) + len(merged))
	buf.Write(targetData[:targetSegment.Start])
	buf.Write(targetSegment.OpenTag)
	buf.Write(merged)
	buf.Write(targetSegment.CloseTag)
	buf.Write(targetData[targetSegment.End:])
	return buf.Bytes()
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
