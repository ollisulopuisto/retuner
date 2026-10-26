package dns

import (
	"testing"
)

func TestShouldIntercept(t *testing.T) {
	s := NewServer([]string{"*.vtuner.com", "example.com"}, "127.0.0.1")

	if !s.shouldIntercept("vtuner.com") {
		t.Error("Expected vtuner.com to be intercepted")
	}

	if !s.shouldIntercept("sub.vtuner.com") {
		t.Error("Expected sub.vtuner.com to be intercepted")
	}

	if !s.shouldIntercept("example.com") {
		t.Error("Expected example.com to be intercepted")
	}

	if s.shouldIntercept("otherexample.com") {
		t.Error("Expected otherexample.com to NOT be intercepted")
	}

	if s.shouldIntercept("fakevtuner.com") {
		t.Error("Expected fakevtuner.com to NOT be intercepted")
	}
}
