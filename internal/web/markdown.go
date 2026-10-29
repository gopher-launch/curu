package web

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/yuin/goldmark"
)

const maxMarkdownBytes = 256 << 10

var markdownRenderer = goldmark.New()

// RenderMarkdown converts bounded coaching Markdown to HTML. Goldmark's safe
// default renderer omits raw HTML and unsafe link destinations; only output
// from that renderer is promoted to trusted template HTML.
func RenderMarkdown(source string) (template.HTML, error) {
	if len(source) > maxMarkdownBytes {
		return "", fmt.Errorf("markdown exceeds %d bytes", maxMarkdownBytes)
	}
	var output bytes.Buffer
	if err := markdownRenderer.Convert([]byte(source), &output); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return template.HTML(output.String()), nil // #nosec G203 -- safe renderer output only
}
