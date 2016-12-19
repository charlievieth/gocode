package gocode

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

const TestDirectory = "./_testing"

var (
	tests    []Test
	conf     *Config
	testConf *TestConfig
)

type TestConfig struct {
	c  Config
	mu sync.RWMutex
}

func (t *TestConfig) Config() *Config {
	t.mu.RLock()
	c := t.c
	t.mu.RUnlock()
	return &c
}

func (t *TestConfig) SetGOPATH(s string) *Config {
	t.mu.Lock()
	t.c.GOPATH = s
	c := t.c
	t.mu.Unlock()
	return &c
}

func init() {
	var err error
	conf, err = newConfig()
	if err != nil {
		panic(err)
	}
	testConf = &TestConfig{c: *conf}
	tests, err = loadTests()
	if err != nil {
		panic(err)
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

func TestGocode(t *testing.T) {
	for _, test := range tests {
		if err := test.Check(conf); err != nil {
			// t.Fatal(err)
			t.Errorf("%s: %s\n", test.Name, err)
		}
	}
}

func TestGOOS(t *testing.T) {
	conf, err := newConfig()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		conf.GOOS = "windows"
	} else {
		conf.GOOS = "linux"
	}
	if err := tests[0].Check(conf); err != nil {
		t.Error(err)
	}
	if conf.daemon.context.GOOS != conf.GOOS {
		t.Errorf("Expected GOOS to be (%s) got (%s)", conf.GOOS, conf.daemon.context.GOOS)
	}
}

// Test that multiple Configs do not interfere with each other.
func TestMultipleConfig(t *testing.T) {
	var err error
	var confs [3]*Config
	for i := 0; i < len(confs); i++ {
		confs[i], err = newConfig()
		if err != nil {
			t.Fatal(err)
		}
	}
	confs[2].GOPATH = ""

	wg := new(sync.WaitGroup)
	wg.Add(len(confs))
	start := make(chan struct{})
	for i := range confs {
		go func(c *Config) {
			defer wg.Done()
			<-start
			for _, test := range tests {
				if err := test.Check(c); err != nil {
					t.Error(err)
				}
			}
		}(confs[i])
	}
	close(start)
	wg.Wait()

	if confs[2].daemon.context.GOPATH != confs[2].GOPATH {
		t.Errorf("Expected GOPATH to equal (%q) got (%q)", confs[2].GOPATH,
			confs[2].daemon.context.GOPATH)
	}
}

// Parallel stress test.
func TestParallel_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("Remove '-short' flag to run")
	}
	conf := testConf.Config()
	wg := new(sync.WaitGroup)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				for _, test := range tests {
					if err := test.Check(conf); err != nil {
						t.Error(err)
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestParallel_1(t *testing.T) {
	conf := testConf.Config()
	t.Parallel()
	for _, test := range tests {
		if err := test.Check(conf); err != nil {
			t.Error(err)
		}
	}
}

func TestParallel_2(t *testing.T) {
	conf := testConf.Config()
	t.Parallel()
	for _, test := range tests {
		if err := test.Check(conf); err != nil {
			t.Error(err)
		}
	}
}

func TestParallel_3(t *testing.T) {
	testConf.SetGOPATH("")
	conf := testConf.Config()
	t.Parallel()
	for _, test := range tests {
		if err := test.Check(conf); err != nil {
			t.Error(err)
		}
	}
}

func TestParallel_4(t *testing.T) {
	testConf.SetGOPATH(os.Getenv("GOPATH"))
	conf := testConf.Config()
	t.Parallel()
	for _, test := range tests {
		if err := test.Check(conf); err != nil {
			t.Error(err)
		}
	}
}

// Ensure complete does not panic!
func TestCompleteRecover(t *testing.T) {
	d := newDaemon()
	defer func() {
		if e := recover(); e != nil {
			t.Fatalf("TestCompleteRecover panicked: %+v", e)
		}
	}()
	d.complete(nil, "", 0, nil)
}

func BenchmarkOne(b *testing.B) {
	t := tests[0]
	for i := 0; i < b.N; i++ {
		_ = conf.Complete(t.File, t.Name, t.Cursor)
	}
}

func BenchmarkMod(b *testing.B) {
	for i := 0; i < b.N; i++ {
		t := tests[i%len(tests)]
		_ = conf.Complete(t.File, t.Name, t.Cursor)
	}
}

func BenchmarkTen(b *testing.B) {
	if len(tests) < 10 {
		b.Fatal("Expected 10+ test cases")
	}
	tt := tests[:10]
	for i := 0; i < b.N; i++ {
		for _, t := range tt {
			_ = conf.Complete(t.File, t.Name, t.Cursor)
		}
	}
}

func BenchmarkAll(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, t := range tests {
			_ = conf.Complete(t.File, t.Name, t.Cursor)
		}
	}
}

func BenchmarkParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for _, t := range tests {
				_ = conf.Complete(t.File, t.Name, t.Cursor)
			}
		}
	})
}

type Test struct {
	Name   string
	File   []byte
	Cursor int
	Result []string
}

func (t Test) Check(conf *Config) error {
	if conf == nil {
		return errors.New("Check: nil Config")
	}
	fn := filepath.Base(filepath.Dir(t.Name))
	cs := conf.Complete(t.File, t.Name, t.Cursor)
	if cs == nil {
		return fmt.Errorf("Check: nil Candidates (%+v)", conf)
	}
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
		GOOS:   runtime.GOOS,
	}
	if c.GOROOT == "" {
		return nil, fmt.Errorf("GOROOT must be set")
	}
	if c.GOPATH == "" {
		return nil, fmt.Errorf("GOPATH must be set")
	}
	return &c, nil
}
