package report

import "apigee/internal/proxyxml"

// PrintFlowDiffWithLabels prints flow differences using custom labels.
// Returns true when differences exist.
func PrintFlowDiffWithLabels(diff proxyxml.FlowDiff, leftLabel, rightLabel string) bool {
	if len(diff.Missing) == 0 && len(diff.Extra) == 0 && len(diff.Changed) == 0 {
		return false
	}

	if len(diff.Missing) > 0 {
		printFlowListWithLabel("Flows only in %s:\n", leftLabel, diff.Missing, "-")
	}

	if len(diff.Extra) > 0 {
		printFlowListWithLabel("Flows only in %s:\n", rightLabel, diff.Extra, "+")
	}

	if len(diff.Changed) > 0 {
		printFlowChangesWithLabels(diff.Changed, leftLabel, rightLabel)
	}

	return true
}
