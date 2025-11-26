package apigee

import "testing"

func TestMergeProductProxies(t *testing.T) {
	group := apiOperationSet{
		OperationConfigs: []apiOperationConfig{
			{APIProxy: " Inventory-Management "},
			{APIProxy: "inventory-management"},
			{APISource: "Inventory-Management-Source"},
			{APIProxy: ""},
		},
	}
	out := mergeProductProxies([]string{"legacy-proxy", "inventory-management "}, group)

	expected := []string{"Inventory-Management-Source", "inventory-management", "legacy-proxy"}
	// uniqueSorted sorts lexicographically.
	if len(out) != len(expected) {
		t.Fatalf("unexpected length: got %d want %d (%v)", len(out), len(expected), out)
	}
	for i := range out {
		if out[i] != expected[i] {
			t.Fatalf("unexpected proxies[%d]: got %q want %q; full slice: %v", i, out[i], expected[i], out)
		}
	}
}
