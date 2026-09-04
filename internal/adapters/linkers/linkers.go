package linkers

import (
	"errors"
	"strings"

	"github.com/example/sbomb/internal/adapters/linkers/mapparser"
)

type Record = mapparser.Record
type Result = mapparser.Result
type Kind = mapparser.Kind

var ErrMalformed = mapparser.ErrMalformed

func Sniff(text string) string { return mapparser.Sniff(text) }

func ParseString(text string) Result {
	format := Sniff(text)
	if format == "" {
		return Result{Err: errors.New("unknown linker map format: " + mapparser.ErrMalformed.Error())}
	}
	result := mapparser.Parse(strings.NewReader(text), format)
	return result
}
