package main

import (
	"strings"
	"testing"
)

func TestHeaderViewContainsBrandAndProject(t *testing.T) {
	m := model{width: 80, projectName: "demo", gitBranch: "main"}
	got := m.headerView()
	if !strings.Contains(got, "KAMAL TUI") {
		t.Fatalf("header does not contain brand: %q", got)
	}
	if !strings.Contains(got, "demo :: main") {
		t.Fatalf("header does not contain project label: %q", got)
	}
}

func TestHighlightLogLine(t *testing.T) {
	line := "ERROR GET /health 500 https://example.com failed"
	got := highlightLogLine(line)
	for _, token := range []string{"ERROR", "GET /health", "500", "https://example.com", "failed"} {
		if !strings.Contains(got, token) {
			t.Errorf("highlighted log lost token %q: %q", token, got)
		}
	}
}

func TestMascotArtIsPresent(t *testing.T) {
	if !strings.Contains(mascotArt, "__") || !strings.Contains(mascotArt, "\\__/") {
		t.Fatalf("mascot art is unexpectedly empty: %q", mascotArt)
	}
}
