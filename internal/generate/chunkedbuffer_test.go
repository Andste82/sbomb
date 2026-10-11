package generate

import (
	"bytes"
	"testing"
)

// The blocks join to exactly what was written, across block boundaries and in
// writes of any size, in an array of the document's own size.
func TestChunkedBufferJoinsWhatWasWritten(t *testing.T) {
	var want bytes.Buffer
	var buffer chunkedBuffer
	for _, size := range []int{0, 1, chunkSize - 1, 2, chunkSize, 3*chunkSize + 7, 5} {
		part := bytes.Repeat([]byte{byte('a' + size%26)}, size)
		if n, err := buffer.Write(part); n != size || err != nil {
			t.Fatalf("Write(%d bytes) = %d, %v", size, n, err)
		}
		want.Write(part)
	}
	got := buffer.Bytes()
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("joined %d bytes, want the %d written", len(got), want.Len())
	}
	if cap(got) != len(got) {
		t.Errorf("the joined document has capacity %d for %d bytes", cap(got), len(got))
	}
	var empty chunkedBuffer
	if got := empty.Bytes(); len(got) != 0 {
		t.Errorf("an empty buffer joined to %d bytes", len(got))
	}
}
