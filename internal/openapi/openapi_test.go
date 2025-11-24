package openapi

import "testing"

func TestSlugifyTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"My API", "/my-api"},
		{"   ", "/api"},
		{"Hello_World", "/hello-world"},
	}
	for _, tc := range cases {
		if got := SlugifyTitle(tc.in); got != tc.want {
			t.Fatalf("SlugifyTitle(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestComputeFlowName(t *testing.T) {
	if got := computeFlowName("opId", "/p", "GET"); got != "opId" {
		t.Fatalf("computeFlowName with operationId returned %q", got)
	}
	if got := computeFlowName("", "/p", "GET"); got != "/p get" {
		t.Fatalf("computeFlowName without operationId returned %q", got)
	}
}

func TestBuildFlowsOrdering(t *testing.T) {
	paths := map[string]PathItem{
		"/first": {Get: &Operation{OperationID: "firstGet"}},
		"/last":  {Post: &Operation{OperationID: "lastPost"}},
	}
	order := []OrderedPath{{Path: "/last"}, {Path: "/first"}}
	flows := BuildFlows(paths, order)
	if len(flows) != 2 {
		t.Fatalf("flows len = %d, want 2", len(flows))
	}
	if flows[0].Name != "lastPost" || flows[1].Name != "firstGet" {
		t.Fatalf("unexpected ordering: %#v", flows)
	}
}
