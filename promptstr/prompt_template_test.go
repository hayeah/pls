package promptstr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type FrontMatter struct {
	Title string `json:"title"`
}

func TestSplitFrontmatter(t *testing.T) {
	testCases := []struct {
		name          string
		input         string
		expectedTitle string
		expectedBody  string
		expectedError error
	}{
		{
			name: "valid yaml frontmatter",
			input: `
---
title: Test Title
---
This is the body text.`,
			expectedTitle: "Test Title",
			expectedBody:  "This is the body text.\n",
		},
		// 		{
		// 			name: "different closing delimiter",
		// 			input: `+++
		// title: Test Title
		// ---
		// This is the body text.`,
		// 			expectedOutput: "",
		// 			expectedError:  errors.New("different closing delimiter found"),
		// 		},
		{
			name: "closing delimiter not found",
			input: `---
title: Test Title
This is the body text.`,
			expectedBody:  "",
			expectedError: ErrorClosingDelimiterNotFound,
		},
		{
			name:  "no frontmatter",
			input: "This is the body text.",
			// expectedTitle: "",
			expectedBody: "This is the body text.\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var fm FrontMatter
			body, err := ParseFrontMatter(tc.input, &fm)

			if tc.expectedError != nil {
				assert.ErrorIs(t, err, tc.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedBody, body)
				assert.Equal(t, tc.expectedTitle, fm.Title)
			}
		})
	}
}
