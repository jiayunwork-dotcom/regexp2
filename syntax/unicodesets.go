package syntax

import (
	"unicode"
)

// ECMA-262's Unicode property values that match sequences of code points
// rather than individual code points. String disjunction support is out of
// scope, so these must be rejected instead of resolving as ordinary sets.
var ecmaStringProperties = map[string]bool{
	"RGI_Emoji":                   true,
	"Basic_Emoji":                 true,
	"Emoji_Keycap_Sequence":       true,
	"RGI_Emoji_Modifier_Sequence": true,
	"RGI_Emoji_Flag_Sequence":     true,
	"RGI_Emoji_Tag_Sequence":      true,
	"RGI_Emoji_ZWJ_Sequence":      true,
}

// ClassSetReservedDoublePunctuator from ECMA-262, minus the && and --
// operators which are recognized as set-expression operators before an atom
// is read. Two identical members of this group may not appear where a single
// character atom is expected.
var classReservedDoublePunctuator = map[rune]bool{
	'!': true, '#': true, '$': true, '%': true, '\'': true,
	'(': true, ')': true, '*': true, '+': true, ',': true,
	'.': true, ':': true, ';': true, '<': true, '=': true,
	'>': true, '?': true, '@': true, '^': true, '`': true,
	'{': true, '|': true, '}': true, '~': true,
}

// classAtomV is one parsed ClassAtom of a unicodeSets character class.
type classAtomV struct {
	set         *CharSet
	r           rune // valid when isChar
	isChar      bool
	classEscape rune // shorthand ('d','w', ...) or property ('p','P') escape, 0 otherwise
}

// scanCharSetV parses an ECMAScript /v (unicodeSets) character class. The
// opening bracket has already been consumed; the closing bracket is consumed
// before returning.
func (p *parser) scanCharSetV(caseInsensitive, scanOnly bool) (*CharSet, error) {
	negate := false
	if p.charsRight() > 0 && p.rightChar(0) == '^' {
		p.moveRight(1)
		negate = true
	}

	cc, err := p.scanClassContentsV(caseInsensitive, scanOnly)
	if err != nil {
		return nil, err
	}

	if !scanOnly {
		// The outer '^' complements the fully evaluated set expression.
		cc.negate = negate
	}
	return cc, nil
}

// peekClassSetOperator reports whether the current position starts a && or
// -- set operator.
func (p *parser) peekClassSetOperator() (rune, bool) {
	if p.charsRight() >= 2 && p.rightChar(0) == p.rightChar(1) {
		switch p.rightChar(0) {
		case '&':
			return '&', true
		case '-':
			return '-', true
		}
	}
	return 0, false
}

// scanClassContentsV parses ClassContents: a pure union, a chain of
// intersections, or a chain of subtractions, consuming the closing ']'.
func (p *parser) scanClassContentsV(caseInsensitive, scanOnly bool) (*CharSet, error) {
	cc, count, err := p.scanClassUnionV(caseInsensitive, scanOnly)
	if err != nil {
		return nil, err
	}

	op := rune(0) // '&' for intersection, '-' for subtraction, 0 for none
	for {
		if p.charsRight() == 0 {
			return nil, p.getErr(ErrUnterminatedBracket)
		}
		if p.rightChar(0) == ']' {
			p.moveRight(1)
			return cc, nil
		}

		next, ok := p.peekClassSetOperator()
		if !ok {
			// A union atom directly beside an intersection/subtraction
			// expression mixes the two notations.
			return nil, p.getErr(ErrInvalidClassSetExpression)
		}
		// Each operand of && / -- must be a single ClassAtom. Ranges and
		// multi-atom unions therefore cannot serve as operands.
		if count != 1 {
			return nil, p.getErr(ErrInvalidClassSetExpression)
		}
		if op == 0 {
			op = next
		} else if op != next {
			// && and -- cannot be mixed at the same level.
			return nil, p.getErr(ErrInvalidClassSetExpression)
		}

		p.moveRight(2) // consume the two-character operator

		right, rightCount, err := p.scanClassUnionV(caseInsensitive, scanOnly)
		if err != nil {
			return nil, err
		}
		if rightCount != 1 {
			return nil, p.getErr(ErrInvalidClassSetExpression)
		}

		if !scanOnly {
			if op == '&' {
				cc = intersectCharSet(cc, right)
			} else {
				cc = subtractCharSet(cc, right)
			}
		}
		count = 1
	}
}

// scanClassUnionV parses a run of adjacent ClassAtoms (a ClassUnion,
// including a-b ranges) up to the next ']', '&&' or '--'.
func (p *parser) scanClassUnionV(caseInsensitive, scanOnly bool) (*CharSet, int, error) {
	cc := &CharSet{}
	atomCount := 0
	var prevRune rune
	prevIsChar := false

	for p.charsRight() > 0 {
		if p.rightChar(0) == ']' {
			break
		}
		if _, ok := p.peekClassSetOperator(); ok {
			break
		}

		if p.rightChar(0) == '-' {
			// An unescaped hyphen is only valid as the separator of a range.
			if atomCount == 0 || !prevIsChar || p.charsRight() < 2 {
				return nil, 0, p.getErr(ErrInvalidClassSetExpression)
			}
			p.moveRight(1) // consume '-'
			if p.charsRight() == 0 || p.rightChar(0) == ']' {
				return nil, 0, p.getErr(ErrInvalidClassSetExpression)
			}
			if _, ok := p.peekClassSetOperator(); ok {
				return nil, 0, p.getErr(ErrInvalidClassSetExpression)
			}

			atom, err := p.scanClassAtomV(caseInsensitive, scanOnly)
			if err != nil {
				return nil, 0, err
			}
			if !atom.isChar {
				if atom.classEscape == 'p' || atom.classEscape == 'P' {
					return nil, 0, p.getErr(ErrShorthandClassInCharRange, string(atom.classEscape))
				}
				if atom.classEscape != 0 {
					return nil, 0, p.getErr(ErrBadClassInCharRange, atom.classEscape)
				}
				return nil, 0, p.getErr(ErrInvalidClassSetExpression)
			}
			if prevRune > atom.r {
				return nil, 0, p.getErr(ErrReversedCharRange, prevRune, atom.r)
			}

			if !scanOnly {
				rangeSet := &CharSet{}
				rangeSet.addRange(prevRune, atom.r)
				if caseInsensitive {
					rangeSet.addCaseEquivalences(ECMAScript | Unicode | IgnoreCase)
				}
				cc.addSet(*rangeSet)
			}
			atomCount += 2 // separator dash plus endpoint atom
			prevRune = atom.r
			prevIsChar = true
			continue
		}

		atom, err := p.scanClassAtomV(caseInsensitive, scanOnly)
		if err != nil {
			return nil, 0, err
		}
		if !scanOnly {
			cc.addSet(*atom.set)
		}
		atomCount++
		if atom.isChar {
			prevRune = atom.r
			prevIsChar = true
		} else {
			prevIsChar = false
		}
	}

	return cc, atomCount, nil
}

// scanClassAtomV parses one ClassAtom (a character, a class escape or a
// nested class) at the current position.
func (p *parser) scanClassAtomV(caseInsensitive, scanOnly bool) (classAtomV, error) {
	if p.charsRight() == 0 {
		return classAtomV{}, p.getErr(ErrUnterminatedBracket)
	}
	ch := p.moveRightGetChar()

	if ch == '\\' {
		return p.scanClassEscapeV(caseInsensitive, scanOnly)
	}
	if ch == '[' {
		set, err := p.scanNestedClassV(caseInsensitive, scanOnly)
		if err != nil {
			return classAtomV{}, err
		}
		return classAtomV{set: set}, nil
	}
	if ch == ']' {
		return classAtomV{}, p.getErr(ErrInvalidClassSetExpression)
	}

	switch ch {
	case '(', ')', '{', '}', '/', '|':
		return classAtomV{}, p.getErr(ErrClassReservedPunctuation, ch)
	}

	// A reserved pair of identical punctuators cannot be a character atom.
	if p.charsRight() >= 1 && p.rightChar(0) == ch && classReservedDoublePunctuator[ch] {
		return classAtomV{}, p.getErr(ErrClassReservedDoublePunct, ch, ch)
	}

	var set *CharSet
	if !scanOnly {
		set = &CharSet{}
		set.addChar(ch)
		if caseInsensitive {
			set.addCaseEquivalences(ECMAScript | Unicode | IgnoreCase)
		}
	}
	return classAtomV{set: set, r: ch, isChar: true}, nil
}

// scanNestedClassV parses a nested [ClassContents] class, including its
// optional leading '^'. The opening '[' has already been consumed.
func (p *parser) scanNestedClassV(caseInsensitive, scanOnly bool) (*CharSet, error) {
	negate := false
	if p.charsRight() > 0 && p.rightChar(0) == '^' {
		p.moveRight(1)
		negate = true
	}

	set, err := p.scanClassContentsV(caseInsensitive, scanOnly)
	if err != nil {
		return nil, err
	}
	if negate && !scanOnly {
		set = &CharSet{ranges: complementRanges(flattenRuneSet(set))}
	}
	return set, nil
}

// scanClassEscapeV parses the escape following a backslash inside a
// unicodeSets character class.
func (p *parser) scanClassEscapeV(caseInsensitive, scanOnly bool) (classAtomV, error) {
	if p.charsRight() == 0 {
		return classAtomV{}, p.getErr(ErrIllegalEndEscape)
	}
	ch := p.moveRightGetChar()

	switch ch {
	case 'q':
		return classAtomV{}, p.getErr(ErrClassStringDisjunction)

	case 'd', 'D', 's', 'S', 'w', 'W':
		var set *CharSet
		if !scanOnly {
			set = &CharSet{}
			switch ch {
			case 'd', 'D':
				set.addDigit(true, ch == 'D')
			case 's', 'S':
				set.addSpace(true, false, ch == 'S')
			case 'w', 'W':
				set.addRanges(ecmaWordClass(p.options, ch == 'W').ranges)
			}
		}
		return classAtomV{set: set, classEscape: ch}, nil

	case 'p', 'P':
		prop, err := p.parseProperty()
		if err != nil {
			return classAtomV{}, err
		}
		var set *CharSet
		if !scanOnly {
			set = &CharSet{}
			p.addProperty(set, prop, ch == 'P', caseInsensitive)
		}
		return classAtomV{set: set, classEscape: ch}, nil

	default:
		// Identity and character escapes (\(, \-, \n, \uXXXX, ...) remain
		// ordinary single-character atoms.
		p.moveLeft()
		r, err := p.scanCharEscape(true)
		if err != nil {
			return classAtomV{}, err
		}
		var set *CharSet
		if !scanOnly {
			set = &CharSet{}
			set.addChar(r)
			if caseInsensitive {
				set.addCaseEquivalences(ECMAScript | Unicode | IgnoreCase)
			}
		}
		return classAtomV{set: set, r: r, isChar: true}, nil
	}
}

// intersectCharSet returns the intersection of two character sets.
func intersectCharSet(a, b *CharSet) *CharSet {
	out := &CharSet{ranges: intersectRanges(flattenRuneSet(a), flattenRuneSet(b))}
	out.canonicalize()
	return out
}

// subtractCharSet returns a with every member of b removed.
func subtractCharSet(a, b *CharSet) *CharSet {
	out := &CharSet{ranges: subtractRanges(flattenRuneSet(a), flattenRuneSet(b))}
	out.canonicalize()
	return out
}

// flattenRuneSet resolves categories, outer negation and subtraction into a
// positive, canonical range list over the full Unicode code point domain.
func flattenRuneSet(c *CharSet) []SingleRange {
	ranges := append([]SingleRange(nil), c.ranges...)

	for _, ct := range c.categories {
		member := categoryMemberRanges(ct)
		if ct.Negate {
			ranges = subtractRanges(ranges, member)
		} else {
			ranges = unionRanges(ranges, member)
		}
	}

	if c.negate {
		ranges = complementRanges(ranges)
	}
	if c.sub != nil {
		ranges = subtractRanges(ranges, flattenRuneSet(c.sub))
	}
	return ranges
}

// categoryMemberRanges expands a category into concrete ranges.
func categoryMemberRanges(ct charCategory) []SingleRange {
	if ct.set != nil {
		return flattenRuneSet(ct.set)
	}
	if ct.table != nil {
		return ecmaRangesFromTable(ct.table)
	}
	// The built-in word/space categories never occur in sets produced by
	// unicodeSets parsing, but keeping the algebra total avoids a panic.
	var contains func(rune) bool
	switch ct.Cat {
	case WordCategoryText:
		contains = func(r rune) bool { return IsWordChar(r) != ct.Negate }
	case SpaceCategoryText:
		contains = func(r rune) bool { return unicode.IsSpace(r) != ct.Negate }
	default:
		contains = func(rune) bool { return false }
	}
	return rangesFromMembership(contains)
}

// rangesFromMembership builds canonical ranges from a membership test. It is
// only used for the defensive category fallback above.
func rangesFromMembership(contains func(rune) bool) []SingleRange {
	var out []SingleRange
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !contains(r) {
			continue
		}
		start := r
		for r < unicode.MaxRune && contains(r+1) {
			r++
		}
		out = append(out, SingleRange{First: start, Last: r})
	}
	return out
}

// unionRanges returns the union of two canonical range lists.
func unionRanges(a, b []SingleRange) []SingleRange {
	tmp := CharSet{ranges: append([]SingleRange(nil), a...)}
	tmp.canonicalize()
	tmp.addRanges(b)
	return tmp.ranges
}

// intersectRanges returns the overlap of two sorted, canonical range lists.
func intersectRanges(a, b []SingleRange) []SingleRange {
	out := make([]SingleRange, 0)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		lo := a[i].First
		if b[j].First > lo {
			lo = b[j].First
		}
		hi := a[i].Last
		if b[j].Last < hi {
			hi = b[j].Last
		}
		if lo <= hi {
			out = append(out, SingleRange{First: lo, Last: hi})
		}
		if a[i].Last < b[j].Last {
			i++
		} else {
			j++
		}
	}
	return out
}

// subtractRanges returns the members of canonical range list a that are not
// in canonical range list b.
func subtractRanges(a, b []SingleRange) []SingleRange {
	out := make([]SingleRange, 0)
	j := 0
	for _, ra := range a {
		lo, hi := ra.First, ra.Last
		for j < len(b) && b[j].Last < lo {
			j++
		}
		for k := j; k < len(b) && b[k].First <= hi; k++ {
			if b[k].First > lo {
				out = append(out, SingleRange{First: lo, Last: b[k].First - 1})
			}
			if b[k].Last >= lo {
				lo = b[k].Last + 1
			}
		}
		if lo <= hi {
			out = append(out, SingleRange{First: lo, Last: hi})
		}
	}
	return out
}

// complementRanges returns every code point not covered by ranges.
func complementRanges(ranges []SingleRange) []SingleRange {
	return appendECMAComplement(nil, ranges)
}
