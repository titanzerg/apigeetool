package util

import "testing"

const (
	lenMismatchFmt = "len = %d, want %d"
	gotMismatchFmt = "got[%d]=%q, want %q"
)

func TestTrimStrings(t *testing.T) {
	in := []string{" a ", " ", "", "\t", "b"}
	got := TrimStrings(in)
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf(lenMismatchFmt, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf(gotMismatchFmt, i, got[i], want[i])
		}
	}
}

func TestUniqueSortedStrings(t *testing.T) {
	in := []string{"B", "a", "b", "A", " c "}
	got := UniqueSortedStrings(in)
	want := []string{"B", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf(lenMismatchFmt, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf(gotMismatchFmt, i, got[i], want[i])
		}
	}
}

func TestMergeAndUnique(t *testing.T) {
	base := []string{"a", "b"}
	extra := []string{"B", "c", " ", "A"}
	got := MergeAndUnique(base, extra)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf(lenMismatchFmt, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf(gotMismatchFmt, i, got[i], want[i])
		}
	}
}

func TestUniqueStrings(t *testing.T) {
	in := []string{" A", "a ", "B", "", "b", "c"}
	got := UniqueStrings(in)
	want := []string{"A", "B", "c"}
	if len(got) != len(want) {
		t.Fatalf(lenMismatchFmt, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf(gotMismatchFmt, i, got[i], want[i])
		}
	}
}
