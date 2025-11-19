package update

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// ConfirmApply prompts the user to confirm whether the downloaded ProxyEndpoint should be updated.
func ConfirmApply() (bool, error) {
	return confirmApply(os.Stdin, os.Stdout)
}

// ReplaceProxyEndpoint updates only the <Flows> section of the target file with the generated contents.
func ReplaceProxyEndpoint(generatedPath, targetPath string) error {
	genData, err := os.ReadFile(generatedPath)
	if err != nil {
		return err
	}
	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		return err
	}

	genSegment, err := locateFlowsSegment(genData)
	if err != nil {
		return fmt.Errorf("generated ProxyEndpoint missing <Flows>: %w", err)
	}
	targetSegment, err := locateFlowsSegment(targetData)
	if err != nil {
		return fmt.Errorf("target ProxyEndpoint missing <Flows>: %w", err)
	}

	merged := preserveSpecialFlows(genSegment.Inner, targetSegment.Inner)

	var buf bytes.Buffer
	buf.Grow(len(targetData) - (targetSegment.End - targetSegment.Start) + len(merged))
	buf.Write(targetData[:targetSegment.Start])
	buf.Write(targetSegment.OpenTag)
	buf.Write(merged)
	buf.Write(targetSegment.CloseTag)
	buf.Write(targetData[targetSegment.End:])

	return os.WriteFile(targetPath, buf.Bytes(), 0o644)
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

func preserveSpecialFlows(generatedInner, targetInner []byte) []byte {
	result := append([]byte(nil), generatedInner...)
	if !flowExists(result, "NotFound") {
		if block := extractFlowBlock(targetInner, "NotFound"); len(block) > 0 {
			result = appendFlowBlock(result, block)
		}
	}
	return result
}

func flowExists(data []byte, name string) bool {
	patterns := []string{
		fmt.Sprintf(`name="%s"`, name),
		fmt.Sprintf(`name='%s'`, name),
	}
	lower := strings.ToLower(string(data))
	for _, pattern := range patterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

func extractFlowBlock(data []byte, name string) []byte {
	if len(data) == 0 {
		return nil
	}
	source := string(data)
	lower := strings.ToLower(source)
	patterns := []string{
		fmt.Sprintf(`<flow name="%s"`, strings.ToLower(name)),
		fmt.Sprintf(`<flow name='%s'`, strings.ToLower(name)),
	}
	start := -1
	for _, pattern := range patterns {
		if idx := strings.Index(lower, pattern); idx >= 0 {
			start = idx
			break
		}
	}
	if start < 0 {
		return nil
	}

	for start > 0 {
		if source[start-1] == ' ' || source[start-1] == '\t' {
			start--
			continue
		}
		if source[start-1] == '\r' || source[start-1] == '\n' {
			start--
			break
		}
		break
	}

	rest := source[start:]
	endRel := strings.Index(strings.ToLower(rest), "</flow>")
	if endRel < 0 {
		return nil
	}
	end := start + endRel + len("</Flow>")

	for end < len(source) && (source[end] == '\r' || source[end] == '\n' || source[end] == '\t' || source[end] == ' ') {
		end++
	}

	block := source[start:end]
	return []byte(block)
}

func appendFlowBlock(existing, block []byte) []byte {
	existing = append([]byte(nil), existing...)
	if len(bytes.TrimSpace(existing)) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		existing = append(existing, '\n')
	}
	existing = append(existing, block...)
	if !bytes.HasSuffix(existing, []byte("\n")) {
		existing = append(existing, '\n')
	}
	return existing
}
