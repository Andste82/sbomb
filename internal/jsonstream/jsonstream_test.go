package jsonstream

import (
	"bytes"
	"encoding/json"
	"testing"
)

type item struct {
	Name  string            `json:"name"`
	Tags  []string          `json:"tags,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
	Inner *item             `json:"inner,omitempty"`
}

type whole struct {
	Version int64   `json:"version"`
	Label   string  `json:"label"`
	Items   []item  `json:"items"`
	Empty   []item  `json:"empty"`
	Absent  []item  `json:"absent"`
	Numbers []int   `json:"numbers"`
	Nested  [][]int `json:"nested"`
}

// WriteObject writes what json.Encoder writes for the same value, with the
// indentation and escaping sbomb uses, whatever the members hold.
func TestWriteObjectWritesWhatTheEncoderWrites(t *testing.T) {
	value := whole{
		Version: 1,
		Label:   "<a & b>   ü \"q\"",
		Items: []item{
			{Name: "first", Tags: []string{"x", "<y>"}, Attrs: map[string]string{"b": "2", "a": "1"}},
			{Name: "second", Inner: &item{Name: "inner", Tags: []string{}}},
		},
		Empty:   []item{},
		Numbers: []int{3, 1, 2},
		Nested:  [][]int{{1}, {}, nil},
	}
	var want bytes.Buffer
	encoder := json.NewEncoder(&want)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	err := WriteObject(&got, []Member{
		{Name: "version", Value: value.Version},
		{Name: "label", Value: value.Label},
		Elements("items", value.Items),
		Elements("empty", value.Empty),
		Elements("absent", value.Absent),
		Elements("numbers", value.Numbers),
		Elements("nested", value.Nested),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Errorf("WriteObject wrote\n%s\nwant\n%s", got.String(), want.String())
	}
}

func TestWriteObjectWithoutMembers(t *testing.T) {
	var got bytes.Buffer
	if err := WriteObject(&got, nil); err != nil {
		t.Fatal(err)
	}
	if got.String() != "{}\n" {
		t.Errorf("WriteObject() = %q, want the encoder's {}", got.String())
	}
}

// An element that cannot be encoded fails the write rather than leaving a
// truncated document that looks complete.
func TestWriteObjectReportsAnElementItCannotEncode(t *testing.T) {
	var got bytes.Buffer
	err := WriteObject(&got, []Member{{Name: "bad", Array: true, Len: 1, Element: func(int) any { return make(chan int) }}})
	if err == nil {
		t.Fatal("an unencodable element was written")
	}
}
