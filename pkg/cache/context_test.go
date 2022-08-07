package cache

import (
	"go/build"
	"reflect"
	"sort"
	"testing"
)

func TestContextCacheKey(t *testing.T) {
	const want = "amr64|darwin|/go|/home/go:/home/xgo|gc|sfx|1|0|go1.1,go1.9|b1,b2|t1,t2,t3,t4"
	ctxt := build.Context{
		GOARCH:        "amr64",
		GOOS:          "darwin",
		GOROOT:        "/go",
		GOPATH:        "/home/go:/home/xgo",
		Compiler:      "gc",
		InstallSuffix: "sfx",
		CgoEnabled:    true,
		UseAllFiles:   false,
		BuildTags:     []string{"b2", "b1"},
		ToolTags:      []string{"t3", "t1", "t4", "t2"},
		ReleaseTags:   []string{"go1.9", "go1.1"},
	}
	wantBuildTags := append([]string(nil), ctxt.BuildTags...)
	wantToolTags := append([]string(nil), ctxt.ToolTags...)
	wantReleaseTags := append([]string(nil), ctxt.ReleaseTags...)

	got := ContextCacheKey(&ctxt)
	if got != want {
		t.Errorf("ContextCacheKey\n\tgot:  %q\n\twant: %q", got, want)
	}

	// Make sure we didn't modify the original slices
	if !reflect.DeepEqual(ctxt.BuildTags, wantBuildTags) {
		t.Errorf("Modified BuildTags: got: %q want: %q", ctxt.BuildTags, wantBuildTags)
	}
	if !reflect.DeepEqual(ctxt.ToolTags, wantToolTags) {
		t.Errorf("Modified ToolTags: got: %q want: %q", ctxt.ToolTags, wantToolTags)
	}
	if !reflect.DeepEqual(ctxt.ReleaseTags, wantReleaseTags) {
		t.Errorf("Modified ReleaseTags: got: %q want: %q", ctxt.ReleaseTags, wantReleaseTags)
	}
}

func TestContextCacheKeyDirIgnored(t *testing.T) {
	ctxt1 := build.Default
	ctxt2 := build.Default
	ctxt1.Dir = "/go1"
	ctxt2.Dir = "/go2"
	k1 := ContextCacheKey(&ctxt1)
	k2 := ContextCacheKey(&ctxt2)
	if k1 != k2 {
		t.Errorf("ContextCacheKey should ignore the Context.Dir field: %q vs. %q", k1, k2)
	}
}

func BenchmarkContextCacheKey(b *testing.B) {
	b.Run("Default", func(b *testing.B) {
		ctxt := &build.Default
		for i := 0; i < b.N; i++ {
			ContextCacheKey(ctxt)
		}
	})
	b.Run("Sorted", func(b *testing.B) {
		dupe := build.Default
		ctxt := &dupe
		ctxt.BuildTags = append([]string(nil), dupe.BuildTags...)
		ctxt.ToolTags = append([]string(nil), dupe.ToolTags...)
		ctxt.ReleaseTags = append([]string(nil), dupe.ReleaseTags...)
		sort.Strings(ctxt.BuildTags)
		sort.Strings(ctxt.ToolTags)
		sort.Strings(ctxt.ReleaseTags)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ContextCacheKey(ctxt)
		}
	})
}

func BenchmarkContextCacheKeyID(b *testing.B) {
	const key = "amr64|darwin|/go|/home/go:/home/xgo|gc|sfx|1|0|go1.1,go1.9|b1,b2|t1,t2,t3,t4"
	b.Run("Serial", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ContextCacheKeyID(key)
		}
	})
	b.Run("Parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				ContextCacheKeyID(key)
			}
		})
	})
}

/*
func BenchmarkContextCache(b *testing.B) {
	var osArchPairs = [...]string{
		"darwin/amd64",
		"darwin/arm64",
		"linux/386",
		"linux/amd64",
		"linux/arm",
		"linux/arm64",
		"windows/386",
		"windows/amd64",
		"js/wasm",
	}
	var ctxts [len(osArchPairs)]*build.Context

	for i, pair := range osArchPairs {
		os, arch, _ := strings.Cut(pair, "/")
		dupe := build.Default
		ctxt := &dupe
		ctxt.GOOS = os
		ctxt.GOARCH = arch
		if ctxt.InstallSuffix == "" {
			ctxt.InstallSuffix = "/foo"
		}
		ctxt.BuildTags = []string{"a"}
		ctxts[i] = ctxt
	}

	// m := make(map[ContextKey]struct{})
	// for i := 0; i < b.N; i++ {
	// 	ctxt := ctxts[i%len(ctxts)]
	// 	key := NewContextKey(ctxt)
	// 	if _, ok := m[key]; !ok {
	// 		m[key] = struct{}{}
	// 	}
	// }

	m := make(map[string]struct{})
	for i := 0; i < b.N; i++ {
		ctxt := ctxts[i%len(ctxts)]
		key := ContextCacheKey(ctxt)
		if _, ok := m[key]; !ok {
			m[key] = struct{}{}
		}
	}
}
*/

/*
type ContextKey struct {
	GOARCH        string
	GOOS          string
	GOROOT        string
	GOPATH        string
	Compiler      string
	InstallSuffix string
	BuildTags     string // slice
	ToolTags      string // slice
	ReleaseTags   string // slice
	CgoEnabled    bool
	UseAllFiles   bool
}

func NewContextKey(ctxt *build.Context) *ContextKey {
	var scratch []string
	releaseTags := ctxt.ReleaseTags
	if !stringsAreSorted(releaseTags) {
		scratch = append(scratch[:0], releaseTags...)
		sort.Strings(scratch)
		releaseTags = scratch
	}
	buildTags := ctxt.BuildTags
	if !stringsAreSorted(buildTags) {
		scratch = append(scratch[:0], buildTags...)
		sort.Strings(scratch)
		buildTags = scratch
	}
	toolTags := ctxt.ToolTags
	if !stringsAreSorted(toolTags) {
		scratch = append(scratch[:0], toolTags...)
		sort.Strings(scratch)
		toolTags = scratch
	}
	return &ContextKey{
		GOARCH:        ctxt.GOARCH,
		GOOS:          ctxt.GOOS,
		GOROOT:        ctxt.GOROOT,
		GOPATH:        ctxt.GOPATH,
		Compiler:      ctxt.Compiler,
		InstallSuffix: ctxt.InstallSuffix,
		BuildTags:     strings.Join(buildTags, ","),
		ToolTags:      strings.Join(toolTags, ","),
		ReleaseTags:   strings.Join(releaseTags, ","),
		CgoEnabled:    ctxt.CgoEnabled,
		UseAllFiles:   ctxt.UseAllFiles,
	}
}
*/
