package proxyxml

import (
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Flow represents a Flow block inside an Apigee ProxyEndpoint.
type Flow struct {
	Name        string
	Description string
	Condition   string
	RawXML      string
}

// ParseFlowsFromFile parses all Flow definitions from the given ProxyEndpoint XML file.
func ParseFlowsFromFile(path string) ([]Flow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	flows, err := ParseFlows(data)
	if err != nil {
		return nil, fmt.Errorf("parse flows in %s: %w", path, err)
	}
	return flows, nil
}

// ParseFlows parses Flow definitions from raw XML bytes.
func ParseFlows(data []byte) ([]Flow, error) {
	var doc struct {
		Flows []struct {
			Name        string `xml:"name,attr"`
			Description string `xml:"Description"`
			Condition   string `xml:"Condition"`
			InnerXML    string `xml:",innerxml"`
		} `xml:"Flows>Flow"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	result := make([]Flow, 0, len(doc.Flows))
	for _, fl := range doc.Flows {
		result = append(result, Flow{
			Name:        strings.TrimSpace(fl.Name),
			Description: strings.TrimSpace(fl.Description),
			Condition:   strings.TrimSpace(fl.Condition),
			RawXML:      strings.TrimSpace(fl.InnerXML),
		})
	}
	return result, nil
}

// FlowDiff describes differences between two sets of flows.
type FlowDiff struct {
	Missing []Flow
	Extra   []Flow
	Changed []ChangedFlow
}

// ChangedFlow captures per-flow differences.
type ChangedFlow struct {
	Name               string
	GeneratedCondition string
	ExistingCondition  string
	GeneratedDesc      string
	ExistingDesc       string
	ConditionDiff      bool
	DescriptionDiff    bool
}

// DiffFlows compares the generated flows with an existing set.
func DiffFlows(generated, existing []Flow) FlowDiff {
	genMap := make(map[string]Flow, len(generated))
	for _, fl := range generated {
		genMap[fl.Name] = fl
	}

	existingMap := make(map[string]Flow, len(existing))
	for _, fl := range existing {
		existingMap[fl.Name] = fl
	}

	var diff FlowDiff

	for name, genFlow := range genMap {
		if exFlow, ok := existingMap[name]; !ok {
			diff.Missing = append(diff.Missing, genFlow)
		} else {
			condDiff := normalizeWhitespace(genFlow.Condition) != normalizeWhitespace(exFlow.Condition)
			descDiff := normalizeWhitespace(genFlow.Description) != normalizeWhitespace(exFlow.Description)
			if condDiff || descDiff {
				diff.Changed = append(diff.Changed, ChangedFlow{
					Name:               name,
					GeneratedCondition: genFlow.Condition,
					ExistingCondition:  exFlow.Condition,
					GeneratedDesc:      genFlow.Description,
					ExistingDesc:       exFlow.Description,
					ConditionDiff:      condDiff,
					DescriptionDiff:    descDiff,
				})
			}
		}
	}

	for name, exFlow := range existingMap {
		if _, ok := genMap[name]; !ok {
			diff.Extra = append(diff.Extra, exFlow)
		}
	}

	sortFlows(diff.Missing)
	sortFlows(diff.Extra)
	sortChanged(diff.Changed)

	return diff
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func sortFlows(flows []Flow) {
	sort.Slice(flows, func(i, j int) bool {
		return flows[i].Name < flows[j].Name
	})
}

func sortChanged(changes []ChangedFlow) {
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Name < changes[j].Name
	})
}

// FilterFlows returns a new slice excluding flows that match the predicate.
func FilterFlows(flows []Flow, shouldSkip func(Flow) bool) []Flow {
	if shouldSkip == nil || len(flows) == 0 {
		return flows
	}
	result := make([]Flow, 0, len(flows))
	for _, fl := range flows {
		if shouldSkip(fl) {
			continue
		}
		result = append(result, fl)
	}
	return result
}

// ExtractBasePath reads the BasePath from a ProxyEndpoint XML document.
func ExtractBasePath(data []byte) (string, error) {
	var doc struct {
		HTTPProxyConnection struct {
			BasePath string `xml:"BasePath"`
		} `xml:"HTTPProxyConnection"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	return strings.TrimSpace(doc.HTTPProxyConnection.BasePath), nil
}
