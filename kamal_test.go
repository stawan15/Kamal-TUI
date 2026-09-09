package main

import (
	"context"
	"reflect"
	"testing"
)

func TestWithDest(t *testing.T) {
	if got := withDest([]string{"deploy"}, ""); !reflect.DeepEqual(got, []string{"deploy"}) {
		t.Fatalf("default destination args = %#v", got)
	}
	if got := withDest([]string{"deploy"}, "production"); !reflect.DeepEqual(got, []string{"deploy", "-d", "production"}) {
		t.Fatalf("named destination args = %#v", got)
	}
}

func TestLogArgsForHost(t *testing.T) {
	action, ok := actionByKey("l")
	if !ok {
		t.Fatal("log action not found")
	}
	m := model{logHosts: []string{"", "10.0.0.12"}, logHostIndex: 1}
	want := []string{"app", "logs", "-d", "production", "--hosts", "10.0.0.12"}
	if got := m.logArgs(action, "production", ""); !reflect.DeepEqual(got, want) {
		t.Fatalf("log args = %#v, want %#v", got, want)
	}
}

func TestRunMockKamal(t *testing.T) {
	previous := devMode
	devMode = true
	defer func() { devMode = previous }()

	linesCh := make(chan string, 32)
	doneCh := make(chan error, 1)
	runKamal(context.Background(), "", nil, []string{"app", "logs", "--hosts", "10.0.0.12"}, linesCh, doneCh)

	var lines []string
	for line := range linesCh {
		lines = append(lines, line)
	}
	if len(lines) != 23 {
		t.Fatalf("got %d mock lines, want 23: %#v", len(lines), lines)
	}
	if err := <-doneCh; err != nil {
		t.Fatalf("mock command returned error: %v", err)
	}
	if lines[1] != "[dev] target: 10.0.0.12" {
		t.Errorf("target line = %q", lines[1])
	}
}
