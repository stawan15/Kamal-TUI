package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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

func TestFooterDoesNotDuplicateStatus(t *testing.T) {
	m := model{width: 120, statusLine: okStyle.Render("done")}
	footer := m.footerView()
	if strings.Count(ansiEscapePattern.ReplaceAllString(footer, ""), "done") != 1 {
		t.Fatalf("footer duplicated status: %q", footer)
	}
}

func TestRunningLogCanBeCancelled(t *testing.T) {
	cancelCalled := false
	m := model{
		running:        true,
		selectedAction: actionItem{key: "l"},
		cancel:         func() { cancelCalled = true },
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(model)
	if !cancelCalled {
		t.Fatal("cancel function was not called")
	}
	if got.running {
		t.Fatal("log stream is still marked as running after cancel")
	}
	if !strings.Contains(ansiEscapePattern.ReplaceAllString(got.statusLine, ""), "cancelled") {
		t.Fatalf("status = %q, want cancelled", got.statusLine)
	}
}

func TestStaleLogMessagesAreIgnoredAfterServerSwitch(t *testing.T) {
	m := model{runID: 2, outputBuf: []string{"current"}, viewport: viewport.New(20, 5)}
	updated, _ := m.Update(logLineMsg{runID: 1, line: "stale host output"})
	got := updated.(model)
	if strings.Contains(strings.Join(got.outputBuf, "\n"), "stale host output") {
		t.Fatal("stale log output was appended after switching servers")
	}
}
