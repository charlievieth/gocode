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

const TestDirectory = "../_testing"

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

var (
	tests []Test
	conf  *Config
)

func init() {
	var err error
	conf, err = newConfig()
	if err != nil {
		panic(err)
	}
	tests, err = loadTests()
	if err != nil {
		panic(err)
	}
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

func Benchmark_01(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[1-1])
	}
}

func Benchmark_02(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[2-1])
	}
}

func Benchmark_03(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[3-1])
	}
}

func Benchmark_04(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[4-1])
	}
}

func Benchmark_05(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[5-1])
	}
}

func Benchmark_06(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[6-1])
	}
}

func Benchmark_07(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[7-1])
	}
}

func Benchmark_08(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[8-1])
	}
}

func Benchmark_09(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[9-1])
	}
}

func Benchmark_10(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[10-1])
	}
}

func Benchmark_11(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[11-1])
	}
}

func Benchmark_12(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[12-1])
	}
}

func Benchmark_13(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[13-1])
	}
}

func Benchmark_14(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[14-1])
	}
}

func Benchmark_15(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[15-1])
	}
}

func Benchmark_16(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[16-1])
	}
}

func Benchmark_17(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[17-1])
	}
}

func Benchmark_18(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[18-1])
	}
}

func Benchmark_19(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[19-1])
	}
}

func Benchmark_20(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[20-1])
	}
}

func Benchmark_21(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[21-1])
	}
}

func Benchmark_22(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[22-1])
	}
}

func Benchmark_23(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[23-1])
	}
}

func Benchmark_24(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[24-1])
	}
}

func Benchmark_25(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[25-1])
	}
}

func Benchmark_26(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[26-1])
	}
}

func Benchmark_27(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[27-1])
	}
}

func Benchmark_28(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[28-1])
	}
}

func Benchmark_29(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[29-1])
	}
}

func Benchmark_30(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[30-1])
	}
}

func Benchmark_31(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[31-1])
	}
}

func Benchmark_32(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[32-1])
	}
}

func Benchmark_33(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[33-1])
	}
}

func Benchmark_34(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[34-1])
	}
}

func Benchmark_35(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[35-1])
	}
}

func Benchmark_36(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[36-1])
	}
}

func Benchmark_37(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[37-1])
	}
}

func Benchmark_38(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[38-1])
	}
}

func Benchmark_39(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[39-1])
	}
}

func Benchmark_40(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[40-1])
	}
}

func Benchmark_41(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[41-1])
	}
}

func Benchmark_42(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[42-1])
	}
}

func Benchmark_43(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[43-1])
	}
}

func Benchmark_44(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[44-1])
	}
}

func Benchmark_45(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[45-1])
	}
}

func Benchmark_46(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[46-1])
	}
}

func Benchmark_47(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[47-1])
	}
}

func Benchmark_48(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[48-1])
	}
}

func Benchmark_49(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[49-1])
	}
}

func Benchmark_50(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchTest(tests[50-1])
	}
}

func benchTest(t Test) {
	_ = conf.Complete(t.File, t.Name, t.Cursor)
}
