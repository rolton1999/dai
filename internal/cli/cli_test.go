package cli

import (
	"errors"
	"testing"
)

func TestRunWithoutURL(test *testing.T) {
	if err := Run(nil); !errors.Is(err, errWithoutArgs) {
		test.Fatalf("expected missing argument error, got %v", err)
	}
}

func TestRunWithEmptyURL(test *testing.T) {
	if err := Run([]string{""}); !errors.Is(err, errEmptyLink) {
		test.Fatalf("expected empty URL error, got %v", err)
	}
}
