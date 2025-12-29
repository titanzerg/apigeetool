package apigee

import "testing"

const basePathFooBar = "/foo/bar"

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

func TestBasePathContains(t *testing.T) {
	cases := []struct {
		target string
		query  string
		want   bool
	}{
		{basePathFooBar, "foo", true},
		{basePathFooBar + "/", "/foo", true},
		{basePathFooBar, "BAR", true},
		{basePathFooBar, "baz", false},
		{"/", "/", true},
		{"", "foo", false},
		{"/foo", "", false},
	}
	for _, tc := range cases {
		if got := basePathContains(tc.target, tc.query); got != tc.want {
			t.Fatalf("basePathContains(%q, %q)=%v, want %v", tc.target, tc.query, got, tc.want)
		}
	}
}
