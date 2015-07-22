package gocode

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const TestDirectory = "/Users/Charlie/go/src/git.vieth.io/gocode/_testing"

type Test struct {
	Name   string
	File   []byte
	Cursor int
	Result []string
}

func (t Test) Check(conf *Config) error {
	fn := filepath.Base(filepath.Dir(t.Name))
	cs := conf.Complete(t.File, t.Name, t.Cursor)
	if len(cs) != len(t.Result) {
		return fmt.Errorf("count: expected %d got %d: %s", len(t.Result), len(cs), fn)
	}
	for i, c := range cs {
		r := t.Result[i]
		if c.String() != r {
			return fmt.Errorf("candidate: expected '%s' got '%s': %s", r, c, fn)
		}
	}
	return nil
}

func BenchmarkGocode(b *testing.B) {
	conf, err := newConfig()
	if err != nil {
		b.Fatal(err)
	}
	tests, err := loadTests()
	if err != nil {
		b.Fatal(err)
	}
	t := tests[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = conf.Complete(t.File, t.Name, t.Cursor)
	}
}

func TestGocode(t *testing.T) {
	conf, err := newConfig()
	if err != nil {
		t.Fatal(err)
	}
	tests, err := loadTests()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		if err := test.Check(conf); err != nil {
			t.Fatal(err)
		} else {
			t.Logf("Passed: %s", filepath.Base(filepath.Dir(test.Name)))
		}
	}
}

func TestInit(t *testing.T) {
	if _, err := newConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTests(); err != nil {
		t.Fatal(err)
	}
}

func loadTests() ([]Test, error) {
	var tests []Test
	list, err := ioutil.ReadDir(TestDirectory)
	if err != nil {
		return nil, err
	}
	for _, fi := range list {
		if fi.IsDir() {
			test, err := newTest(filepath.Join(TestDirectory, fi.Name()))
			if err != nil {
				return nil, err
			}
			tests = append(tests, *test)
		}
	}
	return tests, nil
}

func newTest(path string) (*Test, error) {
	list, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}
	t := Test{Cursor: -1}
	for _, fi := range list {
		fn := fi.Name()
		switch fn {
		case "test.go.in":
			t.Name = filepath.Join(path, fn)
			t.File, err = ioutil.ReadFile(t.Name)
			if err != nil {
				return nil, err
			}
		case "out.expected":
			t.Result, err = newResult(filepath.Join(path, fn))
			if err != nil {
				return nil, err
			}
		default:
			if strings.HasPrefix(fn, "cursor") {
				n := strings.IndexByte(fn, '.')
				if n == -1 {
					return nil, fmt.Errorf("error parsing cursor file: %s", fn)
				}
				t.Cursor, err = strconv.Atoi(fn[n+1:])
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if t.Cursor == -1 {
		return nil, fmt.Errorf("no cursor file in directory: %s", path)
	}
	if t.Name == "" {
		return nil, fmt.Errorf("no test file in directory: %s", path)
	}
	if t.File == nil {
		return nil, fmt.Errorf("nil test file in directory: %s", path)
	}
	return &t, nil
}

func newResult(path string) ([]string, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := bytes.IndexByte(b, '\n')
	if n == len(b)-1 {
		return []string{}, nil
	}
	var s []string
	for _, b := range bytes.Split(b[n+1:], []byte{'\n'}) {
		if len(b) > 1 {
			s = append(s, string(bytes.TrimSpace(b)))
		}
	}
	return s, nil
}

func newConfig() (*Config, error) {
	c := Config{
		GOROOT: runtime.GOROOT(),
		GOPATH: os.Getenv("GOPATH"),
	}
	if c.GOROOT == "" {
		return nil, fmt.Errorf("GOROOT must be set")
	}
	if c.GOPATH == "" {
		return nil, fmt.Errorf("GOPATH must be set")
	}
	return &c, nil
}
