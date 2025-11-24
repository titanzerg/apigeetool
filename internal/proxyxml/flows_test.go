package proxyxml

import "testing"

func TestDiffFlowsDetectsMissingExtraAndChanged(t *testing.T) {
	gen := []Flow{
		{Name: "A", Condition: "x", Description: "desc"},
		{Name: "B", Condition: "y"},
	}
	existing := []Flow{
		{Name: "A", Condition: "x", Description: "different"},
		{Name: "C", Condition: "z"},
	}

	diff := DiffFlows(gen, existing)

	if len(diff.Missing) != 1 || diff.Missing[0].Name != "B" {
		t.Fatalf("Missing = %#v, want B", diff.Missing)
	}
	if len(diff.Extra) != 1 || diff.Extra[0].Name != "C" {
		t.Fatalf("Extra = %#v, want C", diff.Extra)
	}
	if len(diff.Changed) != 1 || diff.Changed[0].Name != "A" || !diff.Changed[0].DescriptionDiff {
		t.Fatalf("Changed = %#v, want A description diff", diff.Changed)
	}
}
