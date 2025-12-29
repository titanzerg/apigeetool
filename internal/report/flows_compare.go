package report

import (
	"fmt"

	"apigee/internal/proxyxml"
)

// PrintFlowDiffWithLabels prints flow differences using custom labels.
// Returns true when differences exist.
func PrintFlowDiffWithLabels(diff proxyxml.FlowDiff, leftLabel, rightLabel string) bool {
	if len(diff.Missing) == 0 && len(diff.Extra) == 0 && len(diff.Changed) == 0 {
		return false
	}

	if len(diff.Missing) > 0 {
		printFlowListWithLabel("Flows only in %s:\n", leftLabel, diff.Missing)
	}

	if len(diff.Extra) > 0 {
		printFlowListWithLabel("Flows only in %s:\n", rightLabel, diff.Extra)
	}

	if len(diff.Changed) > 0 {
		printFlowChangesWithLabels(diff.Changed, leftLabel, rightLabel)
	}

	return true
}

func printFlowListWithLabel(headerFmt, label string, flows []proxyxml.Flow) {
	fmt.Printf(headerFmt, label)
	for _, fl := range flows {
		fmt.Printf("- %s (%s)\n", fl.Name, fl.Condition)
	}
}

func printFlowChangesWithLabels(changes []proxyxml.ChangedFlow, leftLabel, rightLabel string) {
	fmt.Println("Flows with different Condition/Description:")
	for _, change := range changes {
		if change.ConditionDiff {
			fmt.Printf("- %s condition differs:\n  %s: %s\n  %s: %s\n",
				change.Name, leftLabel, change.GeneratedCondition, rightLabel, change.ExistingCondition)
		}
		if change.DescriptionDiff {
			fmt.Printf("- %s description differs:\n  %s: %s\n  %s: %s\n",
				change.Name, leftLabel, change.GeneratedDesc, rightLabel, change.ExistingDesc)
		}
	}
}
