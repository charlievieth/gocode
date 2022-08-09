package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadGoNames(t *testing.T) {
	tempdir := t.TempDir()
	for _, name := range []string{"x.txt", "f1.go", "f2.go", "f2_test.go"} {
		path := filepath.Join(tempdir, name)
		if err := os.WriteFile(path, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for includeTest, want := range map[bool][]string{
		true:  {"f1.go", "f2.go", "f2_test.go"},
		false: {"f1.go", "f2.go"},
	} {
		got, err := readGoNames(tempdir, includeTest)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("readGoNames(%t) = %q; want: %q", includeTest, got, want)
		}
	}
}

func TestNewMultiError(t *testing.T) {
	t.Run("One", func(t *testing.T) {
		want := errors.New("foo")
		e := NewMultiError(want)
		assert.ErrorIs(t, e, want)
		assert.Nil(t, e.errors)
		assert.Equal(t, want.Error(), e.Error())
	})

	t.Run("Mutliple", func(t *testing.T) {
		want := make([]error, 4)
		for i := range want {
			want[i] = fmt.Errorf("err_%d", i)
		}
		e := NewMultiError(want...)
		assert.Equal(t, want, e.Errors())

		const errMsg = `err_0 (and 3 other errors):
err_1
err_2
err_3`
		assert.Equal(t, errMsg, e.Error())
	})
}

func TestMultiErrorBuilder(t *testing.T) {
	assert.Nil(t, (*MultiErrorBuilder)(nil).ToError())

	var m MultiErrorBuilder
	var wg sync.WaitGroup
	err := errors.New("error")
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Add(err)
		}()
	}
	wg.Wait()

	merr := m.ToError().(*MultiError)
	assert.Len(t, merr.Errors(), 16)
}
