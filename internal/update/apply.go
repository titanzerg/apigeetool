package update

import (
	"bufio"
	"bytes"
	"encoding/xml"
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

	if _, err := locateFlowsSegment(genData); err != nil {
		return fmt.Errorf("generated ProxyEndpoint missing <Flows>: %w", err)
	}
	targetSegment, err := locateFlowsSegment(targetData)
	if err != nil {
		return fmt.Errorf("target ProxyEndpoint missing <Flows>: %w", err)
	}

	generatedFlows, err := parseFlowDetails(genData)
	if err != nil {
		return fmt.Errorf("parse generated ProxyEndpoint flows: %w", err)
	}
	targetFlows, err := parseFlowDetails(targetData)
	if err != nil {
		return fmt.Errorf("parse target ProxyEndpoint flows: %w", err)
	}

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

	var buf bytes.Buffer
	buf.Grow(len(targetData) - (targetSegment.End - targetSegment.Start) + merged.Len())
	buf.Write(targetData[:targetSegment.Start])
	buf.Write(targetSegment.OpenTag)
	buf.Write(merged.Bytes())
	buf.Write(targetSegment.CloseTag)
	buf.Write(targetData[targetSegment.End:])

	formatted, err := formatXML(buf.Bytes())
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

type flowDetails struct {
	Name        string      `xml:"name,attr"`
	Description string      `xml:"Description"`
	Request     flowElement `xml:"Request"`
	Response    flowElement `xml:"Response"`
	Condition   string      `xml:"Condition"`
}

type flowElement struct {
	Start    xml.StartElement
	InnerXML string
	Present  bool
}

func (e *flowElement) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	e.Present = true
	e.Start = start
	var inner struct {
		Data string `xml:",innerxml"`
	}
	if err := d.DecodeElement(&inner, &start); err != nil {
		return err
	}
	e.InnerXML = inner.Data
	return nil
}

func parseFlowDetails(data []byte) ([]flowDetails, error) {
	var doc struct {
		Flows []flowDetails `xml:"Flows>Flow"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	for i := range doc.Flows {
		doc.Flows[i].Name = strings.TrimSpace(doc.Flows[i].Name)
		doc.Flows[i].Description = strings.TrimSpace(doc.Flows[i].Description)
		doc.Flows[i].Condition = strings.TrimSpace(doc.Flows[i].Condition)
	}
	return doc.Flows, nil
}

func mostCommonReqResp(flows []flowDetails) (flowElement, flowElement) {
	type combo struct {
		count    int
		request  flowElement
		response flowElement
	}
	combos := make(map[string]combo)
	for _, fl := range flows {
		key := strings.TrimSpace(fl.Request.InnerXML) + "|" + strings.TrimSpace(fl.Response.InnerXML)
		entry := combos[key]
		entry.count++
		if entry.count == 1 {
			entry.request = fl.Request
			entry.response = fl.Response
		}
		combos[key] = entry
	}
	var (
		bestKey   string
		bestCount int
	)
	for key, entry := range combos {
		if entry.count > bestCount {
			bestKey = key
			bestCount = entry.count
		}
	}
	if bestKey == "" {
		return flowElement{}, flowElement{}
	}
	best := combos[bestKey]
	return best.request, best.response
}

func renderFlow(name, description, condition string, req, resp flowElement) string {
	flowIndent := "        "
	childIndent := flowIndent + "    "
	var buf strings.Builder
	buf.WriteString(flowIndent)
	buf.WriteString("<Flow name=\"")
	buf.WriteString(escapeAttr(name))
	buf.WriteString("\">\n")
	buf.WriteString(childIndent)
	buf.WriteString("<Description>")
	buf.WriteString(escapeText(description))
	buf.WriteString("</Description>\n")
	buf.WriteString(renderElement("Request", req, childIndent))
	buf.WriteByte('\n')
	buf.WriteString(renderElement("Response", resp, childIndent))
	buf.WriteByte('\n')
	buf.WriteString(childIndent)
	buf.WriteString("<Condition>")
	buf.WriteString(escapeText(condition))
	buf.WriteString("</Condition>\n")
	buf.WriteString(flowIndent)
	buf.WriteString("</Flow>\n")
	return buf.String()
}

func renderElement(defaultName string, el flowElement, parentIndent string) string {
	name := defaultName
	if el.Start.Name.Local != "" {
		name = el.Start.Name.Local
	}
	attrs := renderAttributes(el.Start.Attr)
	content := strings.TrimSpace(el.InnerXML)
	if content == "" {
		return fmt.Sprintf("%s<%s%s/>", parentIndent, name, attrs)
	}
	content = reindentInnerXML(content, parentIndent+"    ")
	return fmt.Sprintf("%s<%s%s>\n%s\n%s</%s>", parentIndent, name, attrs, content, parentIndent, name)
}

func renderAttributes(attrs []xml.Attr) string {
	if len(attrs) == 0 {
		return ""
	}
	var buf strings.Builder
	for _, attr := range attrs {
		buf.WriteByte(' ')
		if attr.Name.Space != "" {
			buf.WriteString(attr.Name.Space)
			buf.WriteByte(':')
		}
		buf.WriteString(attr.Name.Local)
		buf.WriteString(`="`)
		buf.WriteString(escapeAttr(attr.Value))
		buf.WriteByte('"')
	}
	return buf.String()
}

func newEmptyElement(tag string) flowElement {
	return flowElement{
		Start:   xml.StartElement{Name: xml.Name{Local: tag}},
		Present: true,
	}
}

func reindentInnerXML(content, indent string) string {
	lines := strings.Split(strings.Trim(content, "\n"), "\n")
	minIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		leading := len(line) - len(trimmed)
		if minIndent == -1 || leading < minIndent {
			minIndent = leading
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = indent
			continue
		}
		if len(line) >= minIndent {
			line = line[minIndent:]
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

func formatXML(raw []byte) ([]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false

	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "    ")

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			// Drop purely whitespace-only text nodes to avoid double-blank lines.
			if strings.TrimSpace(string(t)) == "" {
				continue
			}
			trimmed := xml.CharData(strings.TrimSpace(string(t)))
			if err := enc.EncodeToken(trimmed); err != nil {
				return nil, err
			}
		default:
			if err := enc.EncodeToken(tok); err != nil {
				return nil, err
			}
		}
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func escapeAttr(value string) string {
	var buf strings.Builder
	xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

func escapeText(value string) string {
	var buf strings.Builder
	xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
