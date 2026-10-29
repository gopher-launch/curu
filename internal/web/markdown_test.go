package web

import (
	"strings"
	"testing"
)

func TestRenderMarkdownAllowsFormattingWithoutExecutableContent(t *testing.T) {
	t.Parallel()

	rendered, err := RenderMarkdown("**Coach** [safe](https://example.com) <script>alert(1)</script> [bad](javascript:alert(2))")
	if err != nil {
		t.Fatalf("RenderMarkdown() error = %v", err)
	}
	result := string(rendered)
	if !strings.Contains(result, "<strong>Coach</strong>") || !strings.Contains(result, `href="https://example.com"`) {
		t.Errorf("formatted output = %q", result)
	}
	for _, forbidden := range []string{"<script>", "javascript:"} {
		if strings.Contains(result, forbidden) {
			t.Errorf("rendered output contains %q: %q", forbidden, result)
		}
	}
}

func TestRenderMarkdownRejectsOversizedInput(t *testing.T) {
	t.Parallel()
	if _, err := RenderMarkdown(strings.Repeat("x", maxMarkdownBytes+1)); err == nil {
		t.Fatal("RenderMarkdown() error = nil")
	}
}
