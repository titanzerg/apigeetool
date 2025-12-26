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
		fmt.Printf("Flows only in %s:\n", leftLabel)
		for _, fl := range diff.Missing {
			fmt.Printf("- %s (%s)\n", fl.Name, fl.Condition)
		}
	}

	if len(diff.Extra) > 0 {
		fmt.Printf("Flows only in %s:\n", rightLabel)
		for _, fl := range diff.Extra {
			fmt.Printf("- %s (%s)\n", fl.Name, fl.Condition)
		}
	}

	if len(diff.Changed) > 0 {
		fmt.Println("Flows with different Condition/Description:")
		for _, change := range diff.Changed {
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

	return true
}
