package openapi

import (
	"path"
	"strings"
	"unicode"
)

// PathWarning reports a naming warning for an OpenAPI path.
type PathWarning struct {
	Path       string
	Message    string
	Suggestion string
}

// ValidatePaths checks OpenAPI paths against naming conventions.
// It returns warnings (no errors) so generation can proceed.
func ValidatePaths(paths map[string]PathItem) []PathWarning {
	warnings := make([]PathWarning, 0)
	for p := range paths {
		warnings = append(warnings, validatePath(p)...)
	}
	return warnings
}

func validatePath(p string) []PathWarning {
	var warnings []PathWarning
	if p == "" {
		return warnings
	}

	paramRanges := pathParamRanges(p)

	if !strings.HasPrefix(p, "/") {
		warnings = append(warnings, PathWarning{
			Path:       p,
			Message:    "path should start with '/'",
			Suggestion: "/" + p,
		})
	}

	if p != "/" && strings.HasSuffix(p, "/") {
		warnings = append(warnings, PathWarning{
			Path:       p,
			Message:    "trailing slash creates a distinct resource",
			Suggestion: strings.TrimSuffix(p, "/"),
		})
	}

	if hasUnderscoreOutsideParams(p, paramRanges) {
		warnings = append(warnings, PathWarning{
			Path:       p,
			Message:    "use hyphens instead of underscores for readability",
			Suggestion: replaceOutsideParams(p, paramRanges, "_", "-"),
		})
	}

	if hasUppercaseOutsideParams(p, paramRanges) {
		warnings = append(warnings, PathWarning{
			Path:       p,
			Message:    "path should be lowercase",
			Suggestion: lowerOutsideParams(p, paramRanges),
		})
	}

	if hasApiSegment(p, paramRanges) {
		warnings = append(warnings, PathWarning{
			Path:       p,
			Message:    "avoid '/api' as a resource segment; use API subdomain instead",
			Suggestion: trimApiSegment(p),
		})
	}

	if extSuggestion := stripFileExtension(p); extSuggestion != "" {
		warnings = append(warnings, PathWarning{
			Path:       p,
			Message:    "avoid file extensions in URIs; use content-type headers",
			Suggestion: extSuggestion,
		})
	}

	if verbSuggestion := removeCrudVerbSegment(p); verbSuggestion != "" {
		warnings = append(warnings, PathWarning{
			Path:       p,
			Message:    "avoid CRUD verbs in URIs; use HTTP methods instead",
			Suggestion: verbSuggestion,
		})
	}

	return warnings
}

func hasApiSegment(p string, ranges []paramRange) bool {
	trimmed := stripParamContent(p, ranges)
	if trimmed == "/api" || strings.HasPrefix(trimmed, "/api/") {
		return true
	}
	return strings.Contains(trimmed, "/api/")
}

func trimApiSegment(p string) string {
	if p == "/api" {
		return "/"
	}
	if strings.HasPrefix(p, "/api/") {
		out := strings.TrimPrefix(p, "/api")
		if out == "" {
			return "/"
		}
		return out
	}
	return strings.Replace(p, "/api/", "/", 1)
}

func stripFileExtension(p string) string {
	base := path.Base(p)
	if base == "." || base == "/" {
		return ""
	}
	if strings.HasPrefix(base, "{") && strings.HasSuffix(base, "}") {
		return ""
	}
	if idx := strings.LastIndex(base, "."); idx > 0 {
		withoutExt := base[:idx]
		return strings.TrimSuffix(p, base) + withoutExt
	}
	return ""
}

func removeCrudVerbSegment(p string) string {
	verbs := []string{"create", "read", "update", "delete", "get", "set", "post", "put"}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		if isParamSegment(part) {
			continue
		}
		lower := strings.ToLower(strings.Trim(part, "{}"))
		for _, verb := range verbs {
			if lower == verb || strings.HasPrefix(lower, verb+"-") || strings.HasPrefix(lower, verb+"_") {
				parts = append(parts[:i], parts[i+1:]...)
				out := strings.Join(parts, "/")
				if out == "" {
					out = "/"
				}
				return out
			}
		}
	}
	return ""
}

type paramRange struct {
	start int
	end   int
}

func pathParamRanges(p string) []paramRange {
	var ranges []paramRange
	inParam := false
	var start int
	for i, r := range p {
		switch {
		case r == '{' && !inParam:
			inParam = true
			start = i
		case r == '}' && inParam:
			inParam = false
			ranges = append(ranges, paramRange{start: start, end: i})
		}
	}
	return ranges
}

func isInRanges(idx int, ranges []paramRange) bool {
	for _, r := range ranges {
		if idx >= r.start && idx <= r.end {
			return true
		}
	}
	return false
}

func hasUnderscoreOutsideParams(p string, ranges []paramRange) bool {
	for i, r := range p {
		if r == '_' && !isInRanges(i, ranges) {
			return true
		}
	}
	return false
}

func hasUppercaseOutsideParams(p string, ranges []paramRange) bool {
	for i, r := range p {
		if isInRanges(i, ranges) {
			continue
		}
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func replaceOutsideParams(p string, ranges []paramRange, from, to string) string {
	if from == to {
		return p
	}
	var out strings.Builder
	for i, r := range p {
		if isInRanges(i, ranges) {
			out.WriteRune(r)
			continue
		}
		if string(r) == from {
			out.WriteString(to)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func lowerOutsideParams(p string, ranges []paramRange) string {
	var out strings.Builder
	for i, r := range p {
		if isInRanges(i, ranges) {
			out.WriteRune(r)
			continue
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}

func isParamSegment(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func stripParamContent(p string, ranges []paramRange) string {
	if len(ranges) == 0 {
		return p
	}
	var out strings.Builder
	for i, r := range p {
		if isInRanges(i, ranges) {
			if r == '{' || r == '}' {
				out.WriteRune(r)
			} else {
				out.WriteByte('x')
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
