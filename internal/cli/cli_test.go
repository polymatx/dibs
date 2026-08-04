package cli

import (
	"flag"
	"io"
	"slices"
	"testing"
)

func testFlagSet() (*flag.FlagSet, *string, *bool) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reason := fs.String("reason", "", "")
	all := fs.Bool("all", false, "")
	return fs, reason, all
}

func TestParseMixedFlagsAnywhere(t *testing.T) {
	fs, reason, _ := testFlagSet()
	pos, err := parseMixed(fs, []string{"src/auth", "--reason", "refactor auth", "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if *reason != "refactor auth" || !slices.Equal(pos, []string{"src/auth", "docs"}) {
		t.Fatalf("reason=%q pos=%v", *reason, pos)
	}
}

// Review regression: `--` must end flag parsing, so dash-leading
// positionals ("-1 on that approach") are expressible and `release -- x`
// cannot silently become `release --all`.
func TestParseMixedDoubleDash(t *testing.T) {
	fs, _, all := testFlagSet()
	pos, err := parseMixed(fs, []string{"--", "--all"})
	if err != nil {
		t.Fatal(err)
	}
	if *all {
		t.Fatal("--all after -- must not be parsed as a flag")
	}
	if !slices.Equal(pos, []string{"--all"}) {
		t.Fatalf("pos = %v, want [--all]", pos)
	}

	fs2, reason, _ := testFlagSet()
	pos, err = parseMixed(fs2, []string{"--reason", "x", "--", "-1 on that approach"})
	if err != nil {
		t.Fatal(err)
	}
	if *reason != "x" || !slices.Equal(pos, []string{"-1 on that approach"}) {
		t.Fatalf("reason=%q pos=%v", *reason, pos)
	}
}
