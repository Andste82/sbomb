package sbomwriter

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// SourceDateEpoch reads SOURCE_DATE_EPOCH, the build-time pin of section 29,
// as an RFC 3339 UTC time. Unset is ("", nil). A value that is not a whole
// number of seconds is an error rather than silence, so that a caller which
// must state a pinned time can tell "nobody pinned one" from "somebody tried
// and it cannot be read"; a caller that only falls back to the clock may
// treat both alike.
//
// A number of seconds outside the years 0000 to 9999 is unreadable in the same
// sense: RFC 3339, and the DateTime of every format sbomb writes, has exactly
// four year digits, so such a pin formats to a time no document can state. It
// is refused here, where a caller can still refuse the run before discovery,
// rather than found after it by the document's own validation.
func SourceDateEpoch() (string, error) {
	value := os.Getenv("SOURCE_DATE_EPOCH")
	if value == "" {
		return "", nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return "", fmt.Errorf("SOURCE_DATE_EPOCH %q is not a whole number of seconds", value)
	}
	if seconds < minSourceDateEpoch || seconds > maxSourceDateEpoch {
		return "", fmt.Errorf("SOURCE_DATE_EPOCH %q is outside the years 0000 to 9999, which is all an RFC 3339 time can state", value)
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339), nil
}

// The first and last second an RFC 3339 time can state: 0000-01-01T00:00:00Z
// and 9999-12-31T23:59:59Z.
var (
	minSourceDateEpoch = time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	maxSourceDateEpoch = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC).Unix()
)
