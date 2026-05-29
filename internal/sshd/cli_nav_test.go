package sshd

import (
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

func testCLIGroups() []cliGroupEntry {
	return []cliGroupEntry{
		{
			Group: &access.AccessGroup{ID: access.GroupID("home"), Name: "home"},
			Targets: []*access.Target{
				{ID: "t-root", Name: "gateway", Protocol: access.ProtocolSSH, Path: ""},
				{ID: "t-folder", Name: "sw1", Protocol: access.ProtocolSSH, Path: "nullpo7z"},
			},
		},
		{
			Group: &access.AccessGroup{ID: access.GroupID("home/nullpo7z"), Name: "nullpo7z"},
			Targets: []*access.Target{
				{ID: "t-nvme", Name: "x11spi-tf", Protocol: access.ProtocolSSH, Path: ""},
			},
		},
		{
			Group: &access.AccessGroup{ID: access.GroupID("home/pve"), Name: "pve"},
			Targets: []*access.Target{
				{ID: "t-pve", Name: "yesod", Protocol: access.ProtocolSSH, Path: ""},
			},
		},
		{
			Group: &access.AccessGroup{ID: access.GroupID("dev"), Name: "dev"},
		},
	}
}

func TestBuildCLINavView_rootShowsTopLevelGroupsOnly(t *testing.T) {
	view := buildCLINavView(testCLIGroups(), cliNavLocation{})
	if len(view.Items) != 2 {
		t.Fatalf("root items = %d, want 2 (home, dev): %#v", len(view.Items), view.Items)
	}
	if view.Items[0].Label != "dev" || view.Items[1].Label != "home" {
		t.Fatalf("unexpected root order/labels: %#v", view.Items)
	}
}

func TestBuildCLINavView_homeShowsChildGroupsAndPathFolders(t *testing.T) {
	loc := cliNavLocation{GroupPath: "home"}
	view := buildCLINavView(testCLIGroups(), loc)
	if len(view.Items) != 2 {
		t.Fatalf("home items = %d, want 2 (nullpo7z, pve): %#v", len(view.Items), view.Items)
	}
	if len(view.Hosts) != 2 {
		t.Fatalf("home hosts = %#v, want gateway + sw1", view.Hosts)
	}
}

func TestCLINavLocation_parent(t *testing.T) {
	loc := cliNavLocation{GroupPath: "home", PathPrefix: "nullpo7z/rack"}
	if got := loc.parent(); got.PathPrefix != "nullpo7z" || got.GroupPath != "home" {
		t.Fatalf("parent path: %#v", got)
	}
	loc = cliNavLocation{GroupPath: "home", PathPrefix: "nullpo7z"}
	if got := loc.parent(); got.PathPrefix != "" || got.GroupPath != "home" {
		t.Fatalf("parent folder: %#v", got)
	}
	if got := loc.parent().parent(); !got.atRoot() {
		t.Fatalf("parent group: %#v", got)
	}
}

func TestTargetIDsInNavScope_homeIncludesDescendants(t *testing.T) {
	loc := cliNavLocation{GroupPath: "home"}
	ids := targetIDsInNavScope(testCLIGroups(), loc)
	for _, want := range []string{"t-root", "t-folder", "t-nvme", "t-pve"} {
		if !ids[want] {
			t.Fatalf("missing %s in scope: %#v", want, ids)
		}
	}
}

func TestTargetIDsInNavScope_rootIncludesAll(t *testing.T) {
	ids := targetIDsInNavScope(testCLIGroups(), cliNavLocation{})
	if len(ids) != 4 {
		t.Fatalf("root scope = %#v", ids)
	}
}

func TestCLINavLocation_cdIndex(t *testing.T) {
	loc := cliNavLocation{GroupPath: "home"}
	next, ok := loc.cdIndex(testCLIGroups(), 1)
	if !ok || next.GroupPath != "home/nullpo7z" {
		t.Fatalf("cd into child group: ok=%v loc=%#v", ok, next)
	}
}
