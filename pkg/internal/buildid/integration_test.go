package buildid

import (
	"go/build"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadFileNative(t *testing.T) {
	gopath := t.TempDir()

	cmdEnv := func() []string {
		env := []string{"GOPATH=" + gopath}
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "GOPATH=") {
				env = append(env, e)
			}
		}
		return env
	}

	files := map[string]string{
		"tiny/tiny.go": "package tiny\n",
		"p/p.go":       "package p\n\nconst (\n\tConst1 = iota + 1\n\tConst2\n\tConst3\n)\n\nvar Var1 = 1\n\nfunc F1() int { return 1 }\n",
	}

	for name, source := range files {
		path := filepath.Join(gopath, "src", name)
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}

		archive := filepath.Join(dir, "out.a")
		cmd := exec.Command("go", "build", "-o", archive)
		cmd.Dir = dir
		cmd.Env = cmdEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("error: %s: %s", err, strings.TrimSpace(string(out)))
		}

		if _, err := ReadFile(archive); err != nil {
			t.Fatal(err)
		}
	}
}

// WARN: put this test behind a flag
func TestReadFileIntegration(t *testing.T) {
	ctxt := build.Default
	if fi, err := os.Stat(ctxt.GOROOT); err != nil || !fi.IsDir() {
		t.Skip("test requires go installation")
	}

	root := filepath.Join(ctxt.GOROOT, "pkg", ctxt.GOOS+"_"+ctxt.GOARCH)

	// If root does not exist attempt to use Import to find the pkg root
	if _, err := os.Stat(root); err != nil {
		pkg, err := ctxt.Import("runtime", ".", build.FindOnly)
		if err != nil {
			t.Fatal(err)
		}
		root = pkg.PkgTargetRoot
	}
	if _, err := os.Stat(root); err != nil {
		t.Skip("test requires GOROOT/pkg")
	}

	numCPU := runtime.NumCPU()
	if numCPU > 4 {
		numCPU = 4
	}
	ch := make(chan string, numCPU*4)
	var wg sync.WaitGroup
	for i := 0; i < numCPU; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range ch {
				id, err := ReadFile(name)
				if err != nil {
					if !os.IsNotExist(err) {
						t.Error(err)
					}
					continue
				}
				if id == "" {
					t.Errorf("%s: empty build id: %q", name, id)
				}
				short, _ := filepath.Rel(root, name)
				t.Logf("%s: %q\n", short, id)
			}
		}()
	}

	count := 0
	start := time.Now()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && strings.HasSuffix(path, ".a") {
			count++
			ch <- path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	close(ch)
	wg.Wait()

	dur := time.Since(start)
	t.Logf("Parsed %d files in %s (%s/op)\n", count, dur, dur/time.Duration(count))
}
