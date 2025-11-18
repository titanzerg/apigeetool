package report

import (
	"fmt"
	"strings"

	"Apigee/internal/proxyxml"
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
		fmt.Println("Flows missing from downloaded proxy (add these):")
		for _, fl := range diff.Missing {
			fmt.Printf("- %s (%s)\n", fl.Name, fl.Condition)
		}
	}

	if len(filteredExtra) > 0 {
		fmt.Println("Flows only in downloaded proxy (remove/rename if not needed):")
		for _, fl := range filteredExtra {
			fmt.Printf("- %s (%s)\n", fl.Name, fl.Condition)
		}
	}

	if len(diff.Changed) > 0 {
		fmt.Println("Flows with different Condition/Description:")
		for _, change := range diff.Changed {
			if change.ConditionDiff {
				fmt.Printf("- %s condition differs:\n  generated: %s\n  existing : %s\n",
					change.Name, change.GeneratedCondition, change.ExistingCondition)
			}
			if change.DescriptionDiff {
				fmt.Printf("- %s description differs:\n  generated: %s\n  existing : %s\n",
					change.Name, change.GeneratedDesc, change.ExistingDesc)
			}
		}
	}

	return true
}
