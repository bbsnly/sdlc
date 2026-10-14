package codemap

// US-089, the edges of the format the first tests left unreached: the bound on
// a heading, a body built by hand that is not its lines, a section on trunk
// whose end cannot be told, and the sentence that says trunk holds a section
// already. Each is a refusal a person reads, so each is held to what it says
// and to the kind the caller decides by, not only to being an error.

import (
	"errors"
	"strings"
	"testing"
)

// A heading goes whole into the record, the commit's message and what the stop
// prints, so it has a bound. One byte over it is refused, and the refusal
// quotes its start rather than all of it; the bound itself is a heading.
func TestAHeadingLongerThanItsBoundIsRefusedByItsStart(t *testing.T) {
	at := strings.Repeat("h", maxHeading)
	if _, err := Parse(cmSet(cmAdd(at, "Keeps h.\n"))); err != nil {
		t.Fatalf("a heading of exactly %d bytes was refused: %v", maxHeading, err)
	}
	over := strings.Repeat("h", maxHeading+1)
	got, err := Parse(cmSet(cmAdd(over, "Keeps h.\n")))
	if err == nil {
		t.Fatalf("a heading of %d bytes was taken: %+v", maxHeading+1, got)
	}
	if !strings.Contains(err.Error(), "200 bytes") || !strings.Contains(err.Error(), strings.Repeat("h", 60)) {
		t.Errorf("the refusal does not give the bound and the heading's start: %v", err)
	}
	if strings.Contains(err.Error(), over) {
		t.Errorf("the refusal quotes the whole heading, which may be a megabyte: %v", err)
	}
}

// A section built by hand is held to what Parse makes of one: its lines and one
// newline. A body with a blank line before or after it, or none at its end,
// would not read back as itself from trunk's file.
func TestApplyRefusesABodyThatIsNotItsLinesAndOneNewline(t *testing.T) {
	for name, body := range map[string]string{
		"a blank line first":    "\nKeeps c.\n",
		"a blank line last":     "Keeps c.\n\n",
		"no newline at its end": "Keeps c.",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Apply([]byte(cmTrunk), []byte(cmTrunk), []Section{{Heading: "c.go", Body: body}}, false)
			cmRefused(t, got, err, "c.go", "one newline")
		})
	}
}

// THREATS T2: a section on trunk that holds a heading written by underlining,
// or a comment left open, cannot be told where it ends. A set that would
// change it is refused with ErrCannotTell, the one kind neither flag of the
// repair answers, whether a person asked to put text over trunk's or not; and
// it is that kind even where another section of the set is only stale.
func TestASectionOnTrunkWhoseEndCannotBeToldIsRefusedWithTheKindNoFlagAnswers(t *testing.T) {
	underlined := strings.Replace(cmTrunk, "Keeps a.\n", "Keeps a.\n\nNotes\n-----\n\nKept by hand.\n", 1)
	opened := strings.Replace(cmTrunk, "Keeps a.\n", "Keeps a.\n<!-- a person's note\n", 1)
	stale := strings.Replace(underlined, "Keeps b.\n", "Keeps b, as another story left it.\n", 1)
	for _, tc := range []struct {
		name, trunk string
		set         []byte
	}{
		{"a replace of a section with an underlined heading in it", underlined, cmSet(cmReplace("a.go", "Keeps a, rewritten.\n"))},
		{"an add of a heading whose section on trunk has an underlined heading", underlined, cmSet(cmAdd("a.go", "Another a.\n"))},
		{"a replace of a section with a comment left open", opened, cmSet(cmReplace("a.go", "Keeps a, rewritten.\n"))},
		{"a stale section beside one that cannot be told", stale, cmSet(cmReplace("b.go", "Keeps b, rewritten.\n"), cmReplace("a.go", "Keeps a, rewritten.\n"))},
	} {
		for _, replace := range []bool{false, true} {
			name := tc.name
			if replace {
				name += ", with replace set"
			}
			t.Run(name, func(t *testing.T) {
				got, err := Apply([]byte(tc.trunk), []byte(cmTrunk), cmParsed(t, tc.set), replace)
				cmRefused(t, got, err, "a.go", "cannot be told")
				if !errors.Is(err, ErrCannotTell) {
					t.Errorf("the refusal is not ErrCannotTell, so a person would be offered a flag that cannot answer it: %v", err)
				}
				if errors.Is(err, ErrDoesNotApply) || errors.Is(err, ErrAlreadyThere) {
					t.Errorf("the refusal is also of another kind: %v", err)
				}
			})
		}
	}
}

// Where trunk holds one section of the set exactly as it was stored and not
// the rest, the set is not there already; it is refused as one that does not
// apply, and the sentence about that section says its text is on trunk, the
// one thing that tells a person nothing of it would be lost.
func TestASectionTrunkHoldsExactlyBesideOneItLacksIsSaidToBeThereAlready(t *testing.T) {
	trunk := cmTrunk + "\n## c.go\n\nKeeps c.\n"
	got, err := Apply([]byte(trunk), []byte(cmTrunk), cmParsed(t, cmSet(cmAdd("c.go", "Keeps c.\n"), cmAdd("d.go", "Keeps d.\n"))), false)
	cmRefused(t, got, err, `"c.go"`, "exactly as it was stored")
	if !errors.Is(err, ErrDoesNotApply) || errors.Is(err, ErrAlreadyThere) {
		t.Errorf("the refusal is %v, want ErrDoesNotApply and not ErrAlreadyThere: d.go is not on trunk", err)
	}
	if strings.Contains(err.Error(), "d.go") {
		t.Errorf("the refusal names d.go, which applies: %v", err)
	}
}
