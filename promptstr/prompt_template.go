package promptstr

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
)

var ErrorClosingDelimiterNotFound = errors.New("closing delimiter not found")

func ParseFrontMatter(input string, v any) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(input))

	var frontmatter bytes.Buffer
	var foundFrontmatter bool
	var processingFrontmatter bool
	var processingBody bool
	var delimiter string

	var body bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()
		if processingBody {
			// copy the rest of the file into body
			fmt.Fprintln(&body, line)
			continue
		}

		trimmedLine := strings.TrimSpace(line)

		if !foundFrontmatter && trimmedLine == "" {
			// skip empty lines at the top of the file that might precede the frontmatter
			continue
		}

		// consider it a frontmatter delimiter if no other line has been read yet
		if trimmedLine == "---" || trimmedLine == "+++" {
			if !foundFrontmatter {
				// open delimiter
				delimiter = trimmedLine
				foundFrontmatter = true
				processingFrontmatter = true
			} else {
				// closing delimiter
				if trimmedLine != delimiter {
					return "", errors.New("different closing delimiter found")
				}

				processingBody = true
				processingFrontmatter = false
			}

			continue
		}

		if foundFrontmatter {
			fmt.Fprintln(&frontmatter, line)
		} else {
			// no frontmatter found, copy the rest of the file into body
			processingBody = true
			fmt.Fprintln(&body, line)
		}

	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	if processingFrontmatter {
		return "", ErrorClosingDelimiterNotFound
	}

	if foundFrontmatter {
		err := yaml.Unmarshal(frontmatter.Bytes(), v)
		if err != nil {
			return "", err
		}
	}

	return body.String(), nil
}
