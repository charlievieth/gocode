package suggest

import (
	"bytes"
	"fmt"
	"os"
	"testing"
)

type benchContext struct {
	data   []byte
	cursor int
}

func newBenchContext(filename string) *benchContext {
	data, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	cursor := bytes.IndexByte(data, '@')
	if cursor < 0 {
		panic(fmt.Sprintf("%s: invalid cursor: %d", filename, cursor))
	}
	return &benchContext{
		data:   bytes.ReplaceAll(data, []byte{'@'}, []byte{}),
		cursor: cursor,
	}
}

var benchdata = map[string]*benchContext{
	"testdata/bench/cursorcontext.go": newBenchContext("testdata/bench/cursorcontext.go"),
}

func BenchmarkNewTokenIterator(b *testing.B) {
	data, err := os.ReadFile("testdata/bench/cursorcontext.go")
	if err != nil {
		b.Fatal(err)
	}
	cursor := bytes.IndexByte(data, '@')
	if cursor < 0 {
		b.Fatal("invalid cursor:", cursor)
	}
	data = bytes.ReplaceAll(data, []byte{'@'}, []byte{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter, _ := newTokenIterator(data, cursor)
		putTokenIterator(iter)
	}

	// it, n := newTokenIterator(data, cursor)
	// b.Log("len:", len(it.tokens))
	// b.Log("n:", n)
}

func BenchmarkDeduceCursorContext(b *testing.B) {
	data, err := os.ReadFile("testdata/bench/cursorcontext.go")
	if err != nil {
		b.Fatal(err)
	}
	cursor := bytes.IndexByte(data, '@')
	if cursor < 0 {
		b.Fatal("invalid cursor:", cursor)
	}
	data = bytes.ReplaceAll(data, []byte{'@'}, []byte{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// _, _, _ := deduceCursorContext(data, cursor)
		deduceCursorContext(data, cursor)
	}
}
