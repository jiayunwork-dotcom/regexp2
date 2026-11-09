package syntax

import (
	"testing"
	"unicode"
)

func parseVSet(t *testing.T, pattern string, opts RegexOptions) *CharSet {
	t.Helper()
	tree, err := Parse(pattern, ParseOptions{RegexOptions: opts})
	if err != nil {
		t.Fatalf("Parse(%q): %v", pattern, err)
	}
	code, err := Write(tree)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(code.Sets) != 1 {
		t.Fatalf("pattern %q produced %d sets, want 1", pattern, len(code.Sets))
	}
	return code.Sets[0]
}

func TestUnicodeSets_ParseRequiresFlags(t *testing.T) {
	_, err := Parse(`[a]`, ParseOptions{RegexOptions: UnicodeSets})
	if err == nil {
		t.Fatal("Parse with UnicodeSets alone: expected error")
	}
	if e, ok := err.(*Error); !ok || e.Code != ErrUnicodeSetsNeedsFlags {
		t.Fatalf("error = %v; want code %q", err, ErrUnicodeSetsNeedsFlags)
	}

	if _, err := Parse(`[a]`, ParseOptions{RegexOptions: ECMAScript | UnicodeSets}); err == nil {
		t.Fatal("Parse with ECMAScript|UnicodeSets: expected error")
	}
	if _, err := Parse(`[a]`, ParseOptions{RegexOptions: Unicode | UnicodeSets}); err == nil {
		t.Fatal("Parse with Unicode|UnicodeSets: expected error")
	}
}

func TestUnicodeSets_SetSemantics(t *testing.T) {
	v := ECMAScript | Unicode | UnicodeSets

	for _, tc := range []struct {
		pattern string
		yes, no string
	}{
		{`[[a-z]--[b-d]--[x]]`, "aezm", "bcdx"},
		{`[[a-z]&&[^aeiou]]`, "bcz", "aeiou0"},
		{`[[a-z]&&[a-c]]`, "abc", "dz0"},
		{`[abc]`, "abc", "d"},
		{`[a-c]`, "abc", "d"},
		{`[]`, "", "a"},
		{`[^]`, "a0\nλ", ""},
		{`[[ab]--[]]`, "ab", "c"},
		{`[[a-c]--[b]]`, "ac", "b"},
		{`[\p{L}&&[^\p{Lu}]]`, "aé", "AÉ0"},
		{`[[\w]&&[^a-c]]`, "d0_", "abc"},
		// Nested classes are single atoms and are legal operands.
		{`[[ab]&&[ab]]`, "ab", "c"},
		// Disjoint intersection collapses to the empty set.
		{`[a&&b]`, "", "ab"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			set := parseVSet(t, tc.pattern, v)
			for _, r := range tc.yes {
				if !set.Contains(r) {
					t.Errorf("Contains(%q) = false, want true", r)
				}
			}
			for _, r := range tc.no {
				if set.Contains(r) {
					t.Errorf("Contains(%q) = true, want false", r)
				}
			}
		})
	}
}

func TestUnicodeSets_FoldThenCombine(t *testing.T) {
	vi := ECMAScript | Unicode | UnicodeSets | IgnoreCase

	for _, tc := range []struct {
		pattern string
		yes, no string
	}{
		// Fold each leaf, then intersect, then the outer complement.
		{`[^[a-z]&&[^aeiou]]`, "aA1", "bBzZ"},
		{`[^a-c]`, "dD1", "aAcC"},
		{`[[a-z]&&[a-b]]`, "aAbB", "cC1"},
		{`[[a-c]--[b]]`, "aAcC", "bB"},
		{`[[a-c]&&[^b]]`, "aAcC", "bB"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			set := parseVSet(t, tc.pattern, vi)
			for _, r := range tc.yes {
				if !set.Contains(r) {
					t.Errorf("Contains(%q) = false, want true", r)
				}
			}
			for _, r := range tc.no {
				if set.Contains(r) {
					t.Errorf("Contains(%q) = true, want false", r)
				}
			}
		})
	}
}

func TestUnicodeSets_ParseErrors(t *testing.T) {
	v := ECMAScript | Unicode | UnicodeSets

	invalid := map[string]ErrorCode{
		`[a&&b--c]`:         ErrInvalidClassSetExpression,
		`[ab&&c]`:           ErrInvalidClassSetExpression,
		`[ab&&cd]`:          ErrInvalidClassSetExpression,
		`[a-z&&b]`:          ErrInvalidClassSetExpression,
		`[b&&a-z]`:          ErrInvalidClassSetExpression,
		`[a-z--b]`:          ErrInvalidClassSetExpression,
		`[[a]-b]`:           ErrInvalidClassSetExpression,
		`[-a]`:              ErrInvalidClassSetExpression,
		`[a-]`:              ErrInvalidClassSetExpression,
		`[a---b]`:           ErrInvalidClassSetExpression,
		`[a&&]`:             ErrInvalidClassSetExpression,
		`[&&a]`:             ErrInvalidClassSetExpression,
		`[a(b]`:             ErrClassReservedPunctuation,
		`[a/b]`:             ErrClassReservedPunctuation,
		`[a|b]`:             ErrClassReservedPunctuation,
		`[a!!b]`:            ErrClassReservedDoublePunct,
		`[a@@b]`:            ErrClassReservedDoublePunct,
		`[a~~b]`:            ErrClassReservedDoublePunct,
		`[\q{a|b}]`:         ErrClassStringDisjunction,
		`[\p{RGI_Emoji}]`:   ErrStringPropertyNotSupported,
		`[\p{Basic_Emoji}]`: ErrStringPropertyNotSupported,
		`[a&&b`:             ErrUnterminatedBracket,
		`[z-a]`:             ErrReversedCharRange,
	}

	for pattern, wantCode := range invalid {
		t.Run(pattern, func(t *testing.T) {
			_, err := Parse(pattern, ParseOptions{RegexOptions: v})
			if err == nil {
				t.Fatalf("Parse(%q): expected error, got nil", pattern)
			}
			if e, ok := err.(*Error); !ok || e.Code != wantCode {
				t.Fatalf("Parse(%q) error = %v; want code %q", pattern, err, wantCode)
			}
		})
	}

	// \q outside character classes is rejected too.
	if _, err := Parse(`\q{a|b}`, ParseOptions{RegexOptions: v}); err == nil {
		t.Fatal(`Parse(\q{a|b}): expected error, got nil`)
	}
}

func TestUnicodeSets_EscapesRemainLiterals(t *testing.T) {
	v := ECMAScript | Unicode | UnicodeSets

	// Each pattern holds two members so the class is not reduced to a single
	// character node.
	for _, tc := range []struct {
		pattern string
		r       rune
	}{
		{`[a\(]`, '('},
		{`[a\)]`, ')'},
		{`[a\{]`, '{'},
		{`[a\}]`, '}'},
		{`[a\/]`, '/'},
		{`[a\|]`, '|'},
		{`[a\-]`, '-'},
		{`[a\!]`, '!'},
		{`[a\-z]`, '-'},
	} {
		set := parseVSet(t, tc.pattern, v)
		if !set.Contains(tc.r) {
			t.Errorf("%q does not contain %q", tc.pattern, tc.r)
		}
		if !set.Contains('a') {
			t.Errorf("%q does not contain a", tc.pattern)
		}
	}
}

func TestUnicodeSets_RangeAlgebra(t *testing.T) {
	full := []SingleRange{{First: 0, Last: 10}}

	got := intersectRanges(full, []SingleRange{{First: 3, Last: 7}})
	if len(got) != 1 || got[0] != (SingleRange{First: 3, Last: 7}) {
		t.Errorf("intersectRanges = %v", got)
	}

	got = subtractRanges(full, []SingleRange{{First: 3, Last: 7}})
	want := []SingleRange{{First: 0, Last: 2}, {First: 8, Last: 10}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("subtractRanges = %v, want %v", got, want)
	}

	got = unionRanges([]SingleRange{{First: 0, Last: 3}}, []SingleRange{{First: 2, Last: 10}})
	if len(got) != 1 || got[0] != (SingleRange{First: 0, Last: 10}) {
		t.Errorf("unionRanges = %v", got)
	}

	got = complementRanges([]SingleRange{{First: 3, Last: 7}})
	if len(got) != 2 || got[0] != (SingleRange{First: 0, Last: 2}) ||
		got[1] != (SingleRange{First: 8, Last: unicode.MaxRune}) {
		t.Errorf("complementRanges = %v", got)
	}

	// Flattening must resolve categories and outer negation.
	cc := &CharSet{}
	cc.addRange('a', 'z')
	flat := flattenRuneSet(cc)
	if len(flat) != 1 || flat[0] != (SingleRange{First: 'a', Last: 'z'}) {
		t.Errorf("flattenRuneSet positive = %v", flat)
	}
	cc.negate = true
	flat = flattenRuneSet(cc)
	if len(flat) != 2 || flat[0].First != 0 || flat[1].Last != unicode.MaxRune {
		t.Errorf("flattenRuneSet negated = %v", flat)
	}
}
