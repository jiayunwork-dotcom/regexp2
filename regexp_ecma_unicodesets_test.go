package regexp2

import "testing"

// Tests for the ECMAScript /v (unicodeSets) compile option and its character
// class set expressions. Legacy behavior without UnicodeSets is covered by
// the existing ECMA test files and must remain unchanged.

func TestUnicodeSets_RequiresECMAScriptAndUnicode(t *testing.T) {
	for _, opts := range []RegexOptions{
		UnicodeSets,
		ECMAScript | UnicodeSets,
		Unicode | UnicodeSets,
		RE2 | UnicodeSets,
	} {
		if _, err := Compile(`a`, opts); err == nil {
			t.Errorf("Compile with options %v: expected error, got nil", opts)
		}
	}

	if _, err := Compile(`a`, ECMAScript|Unicode|UnicodeSets); err != nil {
		t.Errorf("Compile with ECMAScript|Unicode|UnicodeSets: expected success, got %v", err)
	}
}

func TestUnicodeSets_SetOperations(t *testing.T) {
	v := ECMAScript | Unicode | UnicodeSets

	tests := []struct {
		pattern string
		yes     string
		no      string
	}{
		// Chained subtraction, evaluated left to right:
		// [a-z] minus [b-d] minus [x]
		{`^[[a-z]--[b-d]--[x]]$`, "aezm", "cdx0"},
		// Intersection of two nested classes.
		{`^[[a-z]&&[^aeiou]]$`, "bcz", "aeiou0A"},
		// Plain union and ranges still work.
		{`^[abc]$`, "abc", "d"},
		{`^[a-c]$`, "abc", "d"},
		// Nested negation: letters that are not uppercase.
		{`^[\p{L}&&[^\p{Lu}]]$`, "a", "A0"},
		// Repeated intersection at the same level.
		{`^[[a-z]&&[a-m]&&[^c]]$`, "abm", "cnz"},
		// Subtraction with a nested union on the right.
		{`^[[a-z]--[bdf]]$`, "acegz", "bdf"},
		// Nested empty classes.
		{`^[a&&[a]]$`, "a", "b"},
		// Set operations combined with shorthand escapes.
		{`^[[\w]&&[^a-c]]$`, "d0_", "abc"},
		{`^[[a-f]&&[\D]]$`, "af", "3"},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			re, err := Compile(tc.pattern, v)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			for _, input := range tc.yes {
				got, err := re.MatchString(string(input))
				if err != nil {
					t.Fatalf("MatchString(%q): %v", input, err)
				}
				if !got {
					t.Errorf("MatchString(%q) = false; want true", input)
				}
			}
			for _, input := range tc.no {
				got, err := re.MatchString(string(input))
				if err != nil {
					t.Fatalf("MatchString(%q): %v", input, err)
				}
				if got {
					t.Errorf("MatchString(%q) = true; want false", input)
				}
			}
		})
	}
}

func TestUnicodeSets_EmptyClassAndOuterNegation(t *testing.T) {
	v := ECMAScript | Unicode | UnicodeSets

	empty, err := Compile(`^[]$`, v)
	if err != nil {
		t.Fatalf("Compile ^[]$: %v", err)
	}
	for _, input := range []string{"", "a", "\n"} {
		if got, _ := empty.MatchString(input); got {
			t.Errorf("empty class matched %q; want no match", input)
		}
	}

	anything, err := Compile(`^[^]$`, v)
	if err != nil {
		t.Fatalf("Compile ^[^]$: %v", err)
	}
	for _, input := range []string{"a", "0", "\n", "λ"} {
		if got, _ := anything.MatchString(input); !got {
			t.Errorf("negated empty class did not match %q; want match", input)
		}
	}

	// Nested empty set participates in set algebra: a minus nothing is a.
	subEmpty := MustCompile(`^[a--[]]$`, v)
	if got, _ := subEmpty.MatchString("a"); !got {
		t.Error("[a--[]] should still match a")
	}
	if got, _ := subEmpty.MatchString("b"); got {
		t.Error("[a--[]] should not match b")
	}
}

func TestUnicodeSets_IgnoreCase(t *testing.T) {
	vi := ECMAScript | Unicode | UnicodeSets | IgnoreCase

	tests := []struct {
		pattern string
		yes     string
		no      string
	}{
		// Folding happens before the outer complement: a-c folds to A-Ca-c.
		{`^[^a-c]$`, "dD1", "aAcC"},
		// Fold leaves first, then intersect, then outer complement.
		{`^[^[a-z]&&[^aeiou]]$`, "aA1", "bBcCzZ"},
		// Folded intersection of ranges.
		{`^[[a-z]&&[a-b]]$`, "aAbB", "cC1"},
		// Folded subtraction.
		{`^[[a-c]--[b]]$`, "aAcC", "bB"},
		// Folded nested negation.
		{`^[[a-c]&&[^b]]$`, "aAcC", "bB"},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			re, err := Compile(tc.pattern, vi)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			for _, input := range tc.yes {
				got, err := re.MatchString(string(input))
				if err != nil {
					t.Fatalf("MatchString(%q): %v", input, err)
				}
				if !got {
					t.Errorf("MatchString(%q) = false; want true", input)
				}
			}
			for _, input := range tc.no {
				got, err := re.MatchString(string(input))
				if err != nil {
					t.Fatalf("MatchString(%q): %v", input, err)
				}
				if got {
					t.Errorf("MatchString(%q) = true; want false", input)
				}
			}
		})
	}
}

func TestUnicodeSets_CompileErrors(t *testing.T) {
	v := ECMAScript | Unicode | UnicodeSets

	invalid := []string{
		// Mixed operators at the same level.
		`[a&&b--c]`,
		`[a--b&&c]`,
		// Union notation mixed with set operators.
		`[ab&&c]`,
		`[a&&bc]`,
		`[a&&b c]`,
		// Ranges cannot be operands of && / --.
		`[a-z&&b]`,
		`[b&&a-z]`,
		`[a-z--b]`,
		`[b--a-z]`,
		// Shorthand and nested classes cannot be range endpoints.
		`[\w-a]`,
		`[a-\w]`,
		`[\p{L}-a]`,
		`[[a]-b]`,
		// Lone hyphens that are neither a range nor a -- operator.
		`[-a]`,
		`[a-]`,
		`[a---b]`,
		// Unescaped reserved punctuation.
		`[a(b]`,
		`[a)b]`,
		`[a{b]`,
		`[a}b]`,
		`[a/b]`,
		`[a|b]`,
		// Reserved double punctuators.
		`[a!!b]`,
		`[a@@b]`,
		`[a~~b]`,
		`[a##b]`,
		`[a??b]`,
		// String disjunction and string-valued properties.
		`[\q{a|b}]`,
		`[\p{RGI_Emoji}]`,
		`[\P{RGI_Emoji}]`,
		`[\p{Basic_Emoji}]`,
		// Unterminated classes and operators missing operands.
		`[a&&b`,
		`[a&&]`,
		`[a--]`,
		`[&&a]`,
		`[--a]`,
		// Reversed range.
		`[z-a]`,
	}

	for _, pattern := range invalid {
		t.Run(pattern, func(t *testing.T) {
			if _, err := Compile(pattern, v); err == nil {
				t.Errorf("Compile(%q): expected error, got nil", pattern)
			}
		})
	}

	// \q is also rejected outside character classes.
	if _, err := Compile(`\q{a|b}`, v); err == nil {
		t.Error(`Compile(\q{a|b}): expected error, got nil`)
	}
}

func TestUnicodeSets_EscapedPunctuationIsLiteral(t *testing.T) {
	v := ECMAScript | Unicode | UnicodeSets

	for _, tc := range []struct {
		pattern string
		input   rune
	}{
		{`^[\(]$`, '('},
		{`^[\)]$`, ')'},
		{`^[\{]$`, '{'},
		{`^[\}]$`, '}'},
		{`^[\/]$`, '/'},
		{`^[\|]$`, '|'},
		{`^[\-]$`, '-'},
		{`^[\!]$`, '!'},
		{`^[\@]$`, '@'},
	} {
		re, err := Compile(tc.pattern, v)
		if err != nil {
			t.Errorf("Compile(%q): %v", tc.pattern, err)
			continue
		}
		if got, _ := re.MatchString(string(tc.input)); !got {
			t.Errorf("%q did not match %q", tc.pattern, string(tc.input))
		}
		if got, _ := re.MatchString("x"); got {
			t.Errorf("%q unexpectedly matched x", tc.pattern)
		}
	}

	// An escaped hyphen is a literal atom, not a range separator.
	re := MustCompile(`^[a\-z]$`, v)
	for _, input := range []string{"a", "-", "z"} {
		if got, _ := re.MatchString(input); !got {
			t.Errorf("[a\\-z] should match %q", input)
		}
	}
	if got, _ := re.MatchString("b"); got {
		t.Error("[a\\-z] should not match b")
	}
}

func TestUnicodeSets_LegacyBehaviorUnchanged(t *testing.T) {
	// Without UnicodeSets the && sequence keeps its legacy, non-set meaning
	// and the pattern must not start matching consonants.
	legacy := MustCompile(`^[[a-z]&&[^aeiou]]+$`, ECMAScript|Unicode)
	if got, _ := legacy.MatchString("bcd"); got {
		t.Error("legacy pattern unexpectedly matched bcd without UnicodeSets")
	}

	// The same text matches once unicodeSets semantics are enabled.
	vs := MustCompile(`^[[a-z]&&[^aeiou]]+$`, ECMAScript|Unicode|UnicodeSets)
	if got, _ := vs.MatchString("bcd"); !got {
		t.Error("unicodeSets pattern should match bcd")
	}
	if got, _ := vs.MatchString("aeiou"); got {
		t.Error("unicodeSets pattern should not match aeiou")
	}

	// Reserved punctuation stays a literal in a legacy ECMAScript class.
	legacyPunct := MustCompile(`^[(/]$`, ECMAScript|Unicode)
	if got, _ := legacyPunct.MatchString("("); !got {
		t.Error("legacy [(/] should match (")
	}
}
