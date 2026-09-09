package domain

import (
	"fmt"
	"strings"
)

// Conflict is one disagreement, written down so that it can be reported
// instead of settled in silence.
//
// The tool resolves disagreements by priority: the higher-ranked strategy,
// adapter or origin wins, and where nothing outranks anything the contested
// answer is dropped. Either way a reviewer is entitled to know that a second
// answer existed, what it said and where it came from -- section 39.3 asks for
// a finding rather than a log line whenever something affects how far the
// document can be trusted.
//
// The type is deliberately about the report and not about the resolution.
// Nothing here decides anything; a caller has already decided and only
// describes what it did.
type Conflict struct {
	// Field names what the sides disagreed about, in the reader's words
	// rather than in Go's: "source", "owning target", "owning package".
	Field string
	// Subject is the thing the disagreement is about: the object file, the
	// used file, the directory two managers both claim.
	Subject Subject
	// Sides holds every answer that was given, in the order the caller wants
	// them reported. That order is part of the message, so a caller whose
	// answers come out of a map has to sort them first.
	Sides []ConflictSide
	// Winner names the Source of the side that was used. It is empty when no
	// side won: two targets naming one source, two packages claiming one
	// file. Inventing a winner there would be the guess this tool refuses to
	// make, so the report says outright that nothing won.
	Winner string
	// Reason says why the winner won, or why nothing did. It is the half of
	// the report that lets a reviewer decide whether the outcome was right.
	Reason string
}

// ConflictSide is one answer: what was said, and who said it.
type ConflictSide struct {
	// Source is where the value came from -- a strategy name, an adapter, a
	// package manager, a build configuration. A value without its origin is
	// not evidence.
	Source string
	// Value is what that source said.
	Value string
}

// Finding turns the conflict into the diagnostic of section 26.1, under the
// identifier and severity the caller's situation calls for.
//
// The values go into the message and not only into Detail, because no report
// renders Detail: internal/report shows the identifier, the subject, the
// message and the remediation, so a message reading "two sources disagreed"
// would leave a reviewer with nothing to look at. Detail carries the same
// report structured, for --findings-json.
//
// The second result is false when there is nothing to report. Fewer than two
// sides is not a disagreement, and a sentence about a single answer with an
// empty opponent would report something that never happened.
func (c Conflict) Finding(id string, severity Severity) (Finding, bool) {
	if len(c.Sides) < 2 {
		return Finding{}, false
	}
	claims := make([]string, 0, len(c.Sides))
	sides := make([]any, 0, len(c.Sides))
	for _, side := range c.Sides {
		claims = append(claims, fmt.Sprintf("%s says %q", side.Source, side.Value))
		sides = append(sides, map[string]any{"source": side.Source, "value": side.Value})
	}
	outcome := "none of them wins"
	if c.Winner != "" {
		outcome = fmt.Sprintf("%s wins", c.Winner)
	}
	message := fmt.Sprintf("the %s is disputed: %s; %s", c.Field, strings.Join(claims, ", "), outcome)
	if c.Reason != "" {
		message += ", because " + c.Reason
	}
	detail := map[string]any{"field": c.Field, "sides": sides}
	if c.Winner != "" {
		detail["winner"] = c.Winner
	}
	if c.Reason != "" {
		detail["reason"] = c.Reason
	}
	return Finding{
		ID:       id,
		Severity: severity,
		Subject:  c.Subject,
		Message:  message,
		Detail:   detail,
	}, true
}
