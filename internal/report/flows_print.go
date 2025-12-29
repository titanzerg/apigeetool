package report

import (
	"fmt"

	"apigee/internal/proxyxml"
)

func printFlowList(header string, flows []proxyxml.Flow) {
	fmt.Println(header)
	for _, fl := range flows {
		fmt.Printf("- %s (%s)\n", fl.Name, fl.Condition)
	}
}

func printFlowListWithLabel(headerFmt, label string, flows []proxyxml.Flow) {
	fmt.Printf(headerFmt, label)
	for _, fl := range flows {
		fmt.Printf("- %s (%s)\n", fl.Name, fl.Condition)
	}
}

func printFlowChanges(changes []proxyxml.ChangedFlow) {
	fmt.Println("Flows with different Condition/Description:")
	for _, change := range changes {
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
