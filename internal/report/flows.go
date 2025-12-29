package report

import (
	"fmt"
	"strings"

	"apigee/internal/proxyxml"
)

// PrintFlowDiff prints flow differences and returns true when differences exist.
func PrintFlowDiff(diff proxyxml.FlowDiff) bool {
	filteredExtra := proxyxml.FilterFlows(diff.Extra, func(fl proxyxml.Flow) bool {
		return strings.EqualFold(fl.Name, "NotFound")
	})
	if len(diff.Missing) == 0 && len(filteredExtra) == 0 && len(diff.Changed) == 0 {
		fmt.Println("Flows summary: matched 100% (no differences detected).")
		return false
	}

	if len(diff.Missing) > 0 {
		printFlowList("New flows to add into apigee:", diff.Missing)
	}

	if len(filteredExtra) > 0 {
		printFlowList("Missing flows from apigee (remove/rename if not needed):", filteredExtra)
	}

	if len(diff.Changed) > 0 {
		printFlowChanges(diff.Changed)
	}

	return true
}
