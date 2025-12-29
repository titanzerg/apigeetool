package update

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

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

func renderFlow(name, description, condition string, req, resp flowElement) string {
	flowIndent := indentUnit + indentUnit + indentUnit + indentUnit
	childIndent := flowIndent + indentUnit
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
	content = reindentInnerXML(content, parentIndent+indentUnit)
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
