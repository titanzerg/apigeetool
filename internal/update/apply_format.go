package update

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

func formatXML(raw []byte) ([]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false

	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", indentUnit)

	if err := encodeXMLTokens(dec, enc); err != nil {
		return nil, err
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return []byte(normalizeXMLWhitespace(buf.String())), nil
}

func encodeXMLTokens(dec *xml.Decoder, enc *xml.Encoder) error {
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := encodeXMLToken(enc, tok); err != nil {
			return err
		}
	}
}

func encodeXMLToken(enc *xml.Encoder, tok xml.Token) error {
	if text, ok := tok.(xml.CharData); ok {
		// Drop purely whitespace-only text nodes to avoid double-blank lines.
		if strings.TrimSpace(string(text)) == "" {
			return nil
		}
		tok = xml.CharData(strings.TrimSpace(string(text)))
	}
	return enc.EncodeToken(tok)
}

func normalizeXMLWhitespace(out string) string {
	out = strings.ReplaceAll(out, "<Request></Request>", "<Request/>")
	out = strings.ReplaceAll(out, "<Response></Response>", "<Response/>")
	out = strings.ReplaceAll(out, "&#34;", `"`)
	out = strings.ReplaceAll(out, "&quot;", `"`)
	return out
}

func escapeAttr(value string) string {
	return escapeXML(value)
}

func escapeText(value string) string {
	return escapeXML(value)
}

func escapeXML(value string) string {
	var buf strings.Builder
	xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
