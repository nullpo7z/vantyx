package telnetproxy

import "testing"

func TestEncodeNAWS(t *testing.T) {
	got := encodeNAWS(120, 30)
	want := []byte{
		iac, sb, optNAWS,
		0, 120, 0, 30,
		iac, se,
	}
	if len(got) != len(want) {
		t.Fatalf("len %d want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got %d want %d (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestEncodeNAWS_DefaultSize(t *testing.T) {
	got := encodeNAWS(0, 0)
	if got[3] != 0 || got[4] != 80 || got[5] != 0 || got[6] != 24 {
		t.Fatalf("expected 80x24 default, got %v", got)
	}
}

func TestClampTerminalSize(t *testing.T) {
	c, r := clampTerminalSize(70000, 70000)
	if c != 65535 || r != 65535 {
		t.Fatalf("got %d x %d", c, r)
	}
}

func TestNegotiateReply_DO_NAWS(t *testing.T) {
	reply := negotiateReply(do, optNAWS)
	if len(reply) != 3 || reply[0] != iac || reply[1] != will || reply[2] != optNAWS {
		t.Fatalf("expected WILL NAWS, got %v", reply)
	}
}

func TestFilterIAC_DO_NAWS(t *testing.T) {
	var replies [][]byte
	out := filterIAC([]byte{'x', iac, do, optNAWS, 'y'}, func(b []byte) error {
		replies = append(replies, append([]byte(nil), b...))
		return nil
	})
	if string(out) != "xy" {
		t.Fatalf("got %q", out)
	}
	if len(replies) != 1 || replies[0][1] != will || replies[0][2] != optNAWS {
		t.Fatalf("unexpected replies: %v", replies)
	}
}
