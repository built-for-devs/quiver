package client

import (
	"testing"

	"github.com/built-for-devs/quiver/internal/apperr"
)

func TestClassify(t *testing.T) {
	cases := map[string]int{
		// Real server messages.
		"Content piece not found. Provide content_id, slug, or title.": apperr.CodeNotFound,
		"No campaign found matching 'x'. Active campaigns: Unassigned": apperr.CodeNotFound,
		"Invalid arguments for tool save_content: title is required":   apperr.CodeValidation,
		"Unauthorized":                         apperr.CodeAuth,
		"Something unexpected happened":        apperr.CodeGeneral,
		"Found 3 campaigns; no filter applied": apperr.CodeGeneral,
	}
	for msg, want := range cases {
		if got := classify(msg); got != want {
			t.Errorf("classify(%q) = %d, want %d", msg, got, want)
		}
	}
}
