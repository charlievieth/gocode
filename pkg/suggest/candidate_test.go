package suggest

import (
	"strings"
	"testing"
)

func TestHasPrefixCase(t *testing.T) {
	var tests = []struct {
		s, prefix string
	}{
		{"", ""},
		{"a", ""},
		{"", "a"},
		{"a", "A"},
		{"ABC", "abc"},
		{"ABC", "a"},
	}
	for _, test := range tests {
		want := strings.HasPrefix(strings.ToLower(test.s), strings.ToLower(test.prefix))
		got := hasPrefixCase(test.s, test.prefix)
		if want != got {
			t.Errorf("hasPrefixCase(%q, %q) = %t; want: %t", test.s, test.prefix, got, want)
		}
	}
}
