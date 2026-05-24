package httpapi

import "testing"

func TestMergeCommandLine_TabCompletion(t *testing.T) {
	stdin := "ls /u"
	echo := "ls /usr/bin/"
	got := mergeCommandLine(stdin, echo)
	if got != "ls /usr/bin/" {
		t.Fatalf("got %q want ls /usr/bin/", got)
	}
}

func TestMergeCommandLine_WithPrompt(t *testing.T) {
	stdin := "ls /u"
	echo := "user@host:~$ ls /usr/bin/"
	got := mergeCommandLine(stdin, echo)
	if got != "ls /usr/bin/" {
		t.Fatalf("got %q want ls /usr/bin/", got)
	}
}

func TestMergeCommandLine_StdinOnly(t *testing.T) {
	got := mergeCommandLine("echo hi", "")
	if got != "echo hi" {
		t.Fatalf("got %q", got)
	}
}

func TestCommandLineTracker_TabRedraw(t *testing.T) {
	var tr commandLineTracker
	tr.feed([]byte("ls /u"))
	if tr.currentLine() != "ls /u" {
		t.Fatalf("after type: %q", tr.currentLine())
	}
	tr.feed([]byte("\rls /usr/bin/"))
	if got := tr.currentLine(); got != "ls /usr/bin/" {
		t.Fatalf("after tab redraw: %q", got)
	}
}

func TestCommandLineTracker_TabRedrawWithEL(t *testing.T) {
	var tr commandLineTracker
	tr.feed([]byte("ls /u"))
	tr.feed([]byte("\r\x1b[Kls /usr/bin/"))
	if got := tr.currentLine(); got != "ls /usr/bin/" {
		t.Fatalf("after tab redraw with EL: %q", got)
	}
}

func TestCommandLineTracker_TabRedrawSplitEscape(t *testing.T) {
	var tr commandLineTracker
	tr.feed([]byte("ls /u"))
	// ESC sequences may arrive split across stdout reads
	tr.feed([]byte("\r\x1b"))
	tr.feed([]byte("[Kls /usr/bin/"))
	if got := tr.currentLine(); got != "ls /usr/bin/" {
		t.Fatalf("after split escape: %q", got)
	}
}

func TestCommandLineTracker_Backspace(t *testing.T) {
	var tr commandLineTracker
	tr.feed([]byte("abc\x7f"))
	if tr.currentLine() != "ab" {
		t.Fatalf("got %q", tr.currentLine())
	}
}

func TestCommandLineTracker_OverwriteFromCR(t *testing.T) {
	var tr commandLineTracker
	tr.feed([]byte("longer\x0dshort"))
	if tr.currentLine() != "short" {
		t.Fatalf("got %q want short", tr.currentLine())
	}
}
