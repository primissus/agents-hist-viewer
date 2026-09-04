package domain

import (
	"strings"
	"testing"
)

func TestCompileSearchQuery_ANDPlusParens(t *testing.T) {
	got, err := CompileSearchQuery("(term1 + term2) term3", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	want := "term1 term2 term3"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCompileSearchQuery_ImplicitAND(t *testing.T) {
	got, err := CompileSearchQuery("alpha beta", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "alpha beta" {
		t.Fatalf("got %q", got)
	}
}

func TestCompileSearchQuery_OR(t *testing.T) {
	got, err := CompileSearchQuery("foo | bar", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "(foo OR bar)" {
		t.Fatalf("got %q", got)
	}
}

func TestCompileSearchQuery_ORKeyword(t *testing.T) {
	got, err := CompileSearchQuery("foo OR bar", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "(foo OR bar)" {
		t.Fatalf("got %q", got)
	}
}

func TestCompileSearchQuery_Phrase(t *testing.T) {
	got, err := CompileSearchQuery(`"exact phrase"`, SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got != `"exact phrase"` {
		t.Fatalf("got %q", got)
	}
}

func TestCompileSearchQuery_NOT(t *testing.T) {
	got, err := CompileSearchQuery("foo -bar", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "foo (NOT bar)" && got != "foo NOT bar" {
		t.Fatalf("got %q", got)
	}
}

func TestCompileSearchQuery_FuzzySuffix(t *testing.T) {
	got, err := CompileSearchQuery("config~", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "config*") {
		t.Fatalf("expected prefix variant, got %q", got)
	}
	if !strings.Contains(got, " OR ") {
		t.Fatalf("expected OR group, got %q", got)
	}
}

func TestCompileSearchQuery_GlobalFuzzyPrefix(t *testing.T) {
	got, err := CompileSearchQuery("~deploy", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "deploy*") {
		t.Fatalf("expected fuzzy prefix, got %q", got)
	}
}

func TestCompileSearchQuery_FuzzyFlag(t *testing.T) {
	got, err := CompileSearchQuery("auth", SearchOpts{Fuzzy: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "auth*") {
		t.Fatalf("expected fuzzy prefix, got %q", got)
	}
}

func TestCompileSearchQuery_ReservedQuoted(t *testing.T) {
	got, err := CompileSearchQuery(`"OR"`, SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got != `"OR"` {
		t.Fatalf("got %q", got)
	}
}

func TestCompileSearchQuery_UnbalancedParen(t *testing.T) {
	if _, err := CompileSearchQuery("(foo", SearchOpts{}); err == nil {
		t.Fatal("expected error")
	}
}
