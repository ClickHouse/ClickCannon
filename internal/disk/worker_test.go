package disk

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsUnsupportedColumnLayoutError(t *testing.T) {
	mismatchErr := fmt.Errorf("failed to decode block: %w", errors.New("72 (columns) != 16 (target)"))
	if !isUnsupportedColumnLayoutError(mismatchErr) {
		t.Fatalf("expected mismatch error to be recognized")
	}

	if isUnsupportedColumnLayoutError(errors.New("io: read error")) {
		t.Fatalf("unexpected non-mismatch error")
	}
}
