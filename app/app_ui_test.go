package main

import "testing"

func TestAffirmativeAcceptsTheWindowsAnswer(t *testing.T) {
	for _, tc := range []struct {
		sel, yes string
		want     bool
	}{
		{"Yes", "Remove everything", true},
		{"Remove everything", "Remove everything", true},
		{"No", "Remove everything", false},
		{"Cancel", "Remove everything", false},
		{"", "Remove everything", false},
		{"Error", "Remove everything", false},
	} {
		if got := affirmative(tc.sel, tc.yes); got != tc.want {
			t.Errorf("affirmative(%q, %q) = %v, want %v", tc.sel, tc.yes, got, tc.want)
		}
	}
}
