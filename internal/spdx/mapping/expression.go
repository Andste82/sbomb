package mapping

import (
	"fmt"
	"strings"
)

// ParsedExpression is what an SPDX licence expression names. The grammar is
// the one SPDX 2.3 annex D and SPDX 3.0.1 annex B share, so both renderers and
// the validator read expressions through this one parser -- with one 3.x
// extension the 2.3 grammar does not have: an AdditionRef after WITH, the
// custom form of an exception. The parser accepts it because a 3.0.1 document
// may carry it; whether a version may be written with one is the business of
// the licence routing (Options.CustomAdditions), not of the parser.
type ParsedExpression struct {
	// Licenses are the licence identifiers named, LicenseRefs included, in
	// order of appearance. A trailing "+" is not part of the identifier.
	Licenses []string
	// Additions are the operands of WITH: exception identifiers and, in 3.x,
	// AdditionRefs.
	Additions []string
	// LowerCaseOperators says at least one operator was written in lower
	// case. Both annexes allow it; sbomb writes upper case, and holds its own
	// output to that.
	LowerCaseOperators bool
}

// ParseExpression reads an SPDX licence expression and reports what it names,
// or why it is not one.
//
// The grammar, in the order it binds: parentheses, then WITH, then AND, then
// OR. An operator is all upper case or all lower case: SPDX 3.0.1 annex B says
// operators "should be matched in a case-sensitive manner, i.e., letters must
// be all upper case or all lower case", and SPDX 2.3 annex D says the same, so
// "MIT or Apache-2.0" is an expression and "MIT Or Apache-2.0" is three
// identifiers in a row, which is not. Identifiers are checked for shape only
// -- any identifier of the right shape is accepted here, because a licence
// list newer than this build's must not make a document unreadable; whether an
// identifier is on the list is a separate question (licenselist).
func ParseExpression(expression string) (ParsedExpression, error) {
	tokens, spaced, err := tokenize(expression)
	if err != nil {
		return ParsedExpression{}, err
	}
	if len(tokens) == 0 {
		return ParsedExpression{}, fmt.Errorf("the licence expression is empty")
	}
	parser := &expressionParser{tokens: tokens, spaced: spaced}
	if err := parser.or(); err != nil {
		return ParsedExpression{}, fmt.Errorf("licence expression %q: %w", expression, err)
	}
	if parser.position != len(tokens) {
		return ParsedExpression{}, fmt.Errorf("licence expression %q: unexpected %q", expression, tokens[parser.position])
	}
	return parser.result, nil
}

// IsLicenseRef reports whether an identifier is a LicenseRef, local or in
// another document.
func IsLicenseRef(id string) bool { return refShaped(id, "LicenseRef-") }

// IsAdditionRef reports whether an identifier is an AdditionRef, the 3.0 form
// of a custom exception.
func IsAdditionRef(id string) bool { return refShaped(id, "AdditionRef-") }

func refShaped(id, kind string) bool {
	if document, rest, found := strings.Cut(id, ":"); found {
		if !strings.HasPrefix(document, "DocumentRef-") || !isIDString(strings.TrimPrefix(document, "DocumentRef-")) {
			return false
		}
		id = rest
	}
	return strings.HasPrefix(id, kind) && isIDString(strings.TrimPrefix(id, kind))
}

// isIDString is the idstring production: letters, digits, "-" and ".".
func isIDString(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// tokenize splits an expression into its tokens. spaced[i] says white space
// came directly before tokens[i]; the grammar cares about it in one place
// only, a "+" (annex B: "There MUST NOT be white space between a license-id
// and any following +"), but a "+" is a token of its own here, so the fact
// has to be kept beside it rather than read back from the token.
func tokenize(expression string) (tokens []string, spaced []bool, err error) {
	current := strings.Builder{}
	space := false
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			spaced = append(spaced, space)
			current.Reset()
			space = false
		}
	}
	for _, r := range expression {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
			space = true
		case r == '(' || r == ')' || r == '+':
			flush()
			tokens = append(tokens, string(r))
			spaced = append(spaced, space)
			space = false
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == ':':
			current.WriteRune(r)
		default:
			return nil, nil, fmt.Errorf("licence expression %q: %q is not allowed in an expression", expression, r)
		}
	}
	flush()
	return tokens, spaced, nil
}

type expressionParser struct {
	tokens   []string
	spaced   []bool
	position int
	result   ParsedExpression
}

func (p *expressionParser) peek() string {
	if p.position < len(p.tokens) {
		return p.tokens[p.position]
	}
	return ""
}

// operator reports whether the next token is the operator op (given in upper
// case), in either of the two spellings the grammar allows, and records a
// lower-case one.
func (p *expressionParser) operator(op string) bool {
	switch p.peek() {
	case op:
		return true
	case strings.ToLower(op):
		p.result.LowerCaseOperators = true
		return true
	}
	return false
}

func (p *expressionParser) or() error {
	if err := p.and(); err != nil {
		return err
	}
	for p.operator("OR") {
		p.position++
		if err := p.and(); err != nil {
			return err
		}
	}
	return nil
}

func (p *expressionParser) and() error {
	if err := p.with(); err != nil {
		return err
	}
	for p.operator("AND") {
		p.position++
		if err := p.with(); err != nil {
			return err
		}
	}
	return nil
}

func (p *expressionParser) with() error {
	if p.peek() == "(" {
		p.position++
		if err := p.or(); err != nil {
			return err
		}
		if p.peek() != ")" {
			return fmt.Errorf("a parenthesis is not closed")
		}
		p.position++
		return nil
	}
	if err := p.simple(); err != nil {
		return err
	}
	if !p.operator("WITH") {
		return nil
	}
	p.position++
	addition := p.peek()
	if !isOperand(addition) || (strings.Contains(addition, ":") && !IsAdditionRef(addition)) {
		return fmt.Errorf("WITH needs an exception, not %q", addition)
	}
	if strings.HasPrefix(addition, "LicenseRef-") {
		return fmt.Errorf("WITH needs an exception, not the licence %q", addition)
	}
	p.position++
	p.result.Additions = append(p.result.Additions, addition)
	return nil
}

// simple is a licence identifier, optionally followed by "+", or a LicenseRef.
func (p *expressionParser) simple() error {
	id := p.peek()
	if !isOperand(id) {
		if id == "" {
			return fmt.Errorf("the expression ends where a licence was expected")
		}
		return fmt.Errorf("a licence was expected, not %q", id)
	}
	if strings.Contains(id, ":") && !IsLicenseRef(id) {
		return fmt.Errorf("%q is neither a licence identifier nor a LicenseRef", id)
	}
	p.position++
	p.result.Licenses = append(p.result.Licenses, id)
	if p.peek() == "+" {
		if IsLicenseRef(id) {
			return fmt.Errorf("%q is a LicenseRef and takes no \"+\"", id)
		}
		if p.spaced[p.position] {
			// Annex B: "There MUST NOT be white space between a license-id
			// and any following +". "GPL-2.0 +" is not "GPL-2.0+".
			return fmt.Errorf("white space separates %q from its \"+\"", id)
		}
		p.position++
	}
	return nil
}

// isOperand is a token that can stand for a licence or an exception: an
// idstring that is not an operator, or a reference with its document prefix.
func isOperand(token string) bool {
	switch token {
	case "", "AND", "OR", "WITH", "and", "or", "with", "(", ")", "+":
		return false
	}
	if strings.Contains(token, ":") {
		return IsLicenseRef(token) || IsAdditionRef(token)
	}
	return isIDString(token)
}
