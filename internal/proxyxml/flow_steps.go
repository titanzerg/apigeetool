package proxyxml

import (
	"encoding/xml"
	"strings"
)

// FlowSteps captures PreFlow/PostFlow step names.
type FlowSteps struct {
	PreFlowRequest   []string
	PreFlowResponse  []string
	PostFlowRequest  []string
	PostFlowResponse []string
}

// ParseFlowSteps parses PreFlow/PostFlow step names from ProxyEndpoint XML.
func ParseFlowSteps(data []byte) (FlowSteps, error) {
	var doc struct {
		PreFlow  flowPhase `xml:"PreFlow"`
		PostFlow flowPhase `xml:"PostFlow"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return FlowSteps{}, err
	}
	return FlowSteps{
		PreFlowRequest:   trimStepNames(doc.PreFlow.Request.Steps),
		PreFlowResponse:  trimStepNames(doc.PreFlow.Response.Steps),
		PostFlowRequest:  trimStepNames(doc.PostFlow.Request.Steps),
		PostFlowResponse: trimStepNames(doc.PostFlow.Response.Steps),
	}, nil
}

type flowPhase struct {
	Request  flowStepList `xml:"Request"`
	Response flowStepList `xml:"Response"`
}

type flowStepList struct {
	Steps []flowStep `xml:"Step"`
}

type flowStep struct {
	Name string `xml:"Name"`
}

func trimStepNames(steps []flowStep) []string {
	if len(steps) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		name := strings.TrimSpace(step.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}
