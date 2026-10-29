package cli

import (
	"testing"
	"unicode/utf8"
)

func TestMascotLinesLineUp(t *testing.T) {
	for i, line := range mascot {
		if utf8.RuneCountInString(line) != utf8.RuneCountInString(mascot[0]) {
			t.Errorf("mascot line %d is %q: every line must be as wide as the first", i, line)
		}
		if width(paintMascot(line, i)) != utf8.RuneCountInString(line) {
			t.Errorf("painting line %d changed its width", i)
		}
	}
}

func TestLongTextIsCutToFit(t *testing.T) {
	if got := cutLeft("/Users/ada/work/acme/checkout-api", 15); got != "…e/checkout-api" {
		t.Fatalf("got %q", got)
	}
	if got := cut("Why do some WhatsApp messages have no name?", 12); width(got) != 12 {
		t.Fatalf("got %q", got)
	}
	if got := center("hi", 6); got != "  hi  " {
		t.Fatalf("got %q", got)
	}
}
