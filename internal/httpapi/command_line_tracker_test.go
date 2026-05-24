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

func TestStripShellPrompt(t *testing.T) {
	got := stripShellPrompt("user@host:~$ ls -la")
	if got != "ls -la" {
		t.Fatalf("got %q", got)
	}
}

func TestCommandLineTracker_CSICursorAndErase(t *testing.T) {
	var tr commandLineTracker
	tr.feed([]byte("hello"))
	tr.feed([]byte("\x1b[1D")) // back one
	tr.feed([]byte("\x1b[0K")) // clear to end from cursor
	if tr.currentLine() != "hell" {
		t.Fatalf("got %q", tr.currentLine())
	}
	tr.feed([]byte("\x1b[2K")) // clear line
	if tr.currentLine() != "" {
		t.Fatalf("after EL2: %q", tr.currentLine())
	}
}

func TestCommandLineTracker_CSIInsertAndDelete(t *testing.T) {
	var tr commandLineTracker
	tr.feed([]byte("ab"))
	tr.feed([]byte("\x1b[@")) // insert 1 blank at col end
	tr.feed([]byte("c"))
	if tr.currentLine() != "abc" {
		t.Fatalf("got %q", tr.currentLine())
	}
}

func TestMergeCommandLine_EchoOnly(t *testing.T) {
	got := mergeCommandLine("", "root@host# whoami")
	if got != "whoami" {
		t.Fatalf("got %q", got)
	}
}

func TestConsumeEscape_Incomplete(t *testing.T) {
	var tr commandLineTracker
	tr.feed([]byte("\x1b"))
	if len(tr.escBuf) != 1 {
		t.Fatalf("escBuf: %v", tr.escBuf)
	}
	tr.feed([]byte("[Kx"))
	if tr.currentLine() != "x" {
		t.Fatalf("got %q", tr.currentLine())
	}
}
