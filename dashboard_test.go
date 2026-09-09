package main

import (
	"strings"
	"testing"
)

func TestParseDockerStats(t *testing.T) {
	raw := "web-1\t24.6%\t312MiB / 1GiB\t30.5%\t1.2MB / 840kB\t12.4MB / 3.1MB\n" +
		"worker-1\t86.4%\t901MiB / 1GiB\t88.0%\t4.8MB / 2.3MB\t41.7MB / 8.6MB"

	stats := parseDockerStats(raw)
	if len(stats) != 2 {
		t.Fatalf("got %d stats, want 2", len(stats))
	}
	if stats[0].Name != "web-1" || stats[0].MemUsage != "312MiB" || stats[0].MemLimit != "1GiB" {
		t.Fatalf("unexpected first stat: %+v", stats[0])
	}
	if stats[0].StatusLv != "ok" || stats[1].StatusLv != "crit" {
		t.Fatalf("unexpected status levels: %q, %q", stats[0].StatusLv, stats[1].StatusLv)
	}
	if stats[1].NetIn != "4.8MB" || stats[1].BlockOut != "8.6MB" {
		t.Fatalf("unexpected traffic values: %+v", stats[1])
	}
}

func TestContainerStatusLevel(t *testing.T) {
	tests := []struct {
		name       string
		cpu, mem   float64
		wantStatus string
	}{
		{name: "healthy", cpu: 20, mem: 40, wantStatus: "ok"},
		{name: "warning cpu", cpu: 51, mem: 20, wantStatus: "warn"},
		{name: "warning memory", cpu: 20, mem: 71, wantStatus: "warn"},
		{name: "critical cpu", cpu: 81, mem: 20, wantStatus: "crit"},
		{name: "critical memory", cpu: 20, mem: 86, wantStatus: "crit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containerStatusLevel(tt.cpu, tt.mem); got != tt.wantStatus {
				t.Errorf("containerStatusLevel(%v, %v) = %q, want %q", tt.cpu, tt.mem, got, tt.wantStatus)
			}
		})
	}
}

func TestMockDockerStats(t *testing.T) {
	stats := mockDockerStats("production")
	if len(stats) != 3 {
		t.Fatalf("got %d mock containers, want 3", len(stats))
	}
	if stats[0].Host != "dev-production-1" || stats[1].Host != "dev-production-2" || stats[2].Host != "dev-production-3" {
		t.Errorf("unexpected mock hosts: %q, %q, %q", stats[0].Host, stats[1].Host, stats[2].Host)
	}
}

func TestRenderDashboardCardsGroupsServers(t *testing.T) {
	stats := []ContainerStat{
		{Host: "web-1", Name: "app-1", CPUPct: 20, MemPct: 30, StatusLv: "ok"},
		{Host: "web-2", Name: "app-2", CPUPct: 90, MemPct: 88, StatusLv: "crit"},
	}
	view := renderDashboardCards(stats, 120, "production")
	for _, want := range []string{"2 SERVERS", "2 CONTAINERS", "web-1", "web-2", "CRITICAL"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard view does not contain %q", want)
		}
	}
}
