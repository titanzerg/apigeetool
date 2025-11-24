package apigee

import "testing"

func TestNormalizeBasePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"products", "/products"},
		{"/products/", "/products"},
		{" /api/v1/ ", "/api/v1"},
	}
	for _, tc := range cases {
		if got := normalizeBasePath(tc.in); got != tc.want {
			t.Fatalf("normalizeBasePath(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUniqueBasePaths(t *testing.T) {
	in := []bundleEndpoint{
		{BasePath: "/a"},
		{BasePath: "/a"},
		{BasePath: "/b"},
	}
	got := uniqueBasePaths(in)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("unexpected result: %#v", got)
	}
}
