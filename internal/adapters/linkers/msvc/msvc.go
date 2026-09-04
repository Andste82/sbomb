package msvc

import (
	"strings"

	"github.com/example/sbomb/internal/adapters/linkers/mapparser"
)

func ParseString(text string) mapparser.Result {
	return mapparser.Parse(strings.NewReader(text), "msvc")
}
