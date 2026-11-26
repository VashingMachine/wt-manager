package shell

import "testing"

func TestShellQuote(t *testing.T) {
	got := Quote("/tmp/worktree with 'quote'")
	want := `'/tmp/worktree with '\''quote'\'''`
	if got != want {
		t.Fatalf("Quote() = %q, want %q", got, want)
	}
}

func TestAppleScriptString(t *testing.T) {
	got := AppleScriptString("say \"hi\"\nnext")
	want := `"say \"hi\"\nnext"`
	if got != want {
		t.Fatalf("AppleScriptString() = %q, want %q", got, want)
	}
}
