package cache

import (
	"go/build"
	"sync"
	"unsafe"
)

// TODO(cev): we might want to change some things here and
// to also use faster ReadDir and HasSubdir funcs.
//
// PackedContext is a copy of build.Context without the func fields.
//
// TODO(mdempsky): Not sure this belongs here.
type PackedContext struct {
	GOARCH        string
	GOOS          string
	GOROOT        string
	GOPATH        string
	CgoEnabled    bool
	UseAllFiles   bool
	Compiler      string
	BuildTags     []string
	ToolTags      []string
	ReleaseTags   []string
	InstallSuffix string
}

func PackContext(ctx *build.Context) PackedContext {
	return PackedContext{
		GOARCH:        ctx.GOARCH,
		GOOS:          ctx.GOOS,
		GOROOT:        ctx.GOROOT,
		GOPATH:        ctx.GOPATH,
		CgoEnabled:    ctx.CgoEnabled,
		UseAllFiles:   ctx.UseAllFiles,
		Compiler:      ctx.Compiler,
		BuildTags:     ctx.BuildTags,
		ToolTags:      ctx.ToolTags,
		ReleaseTags:   ctx.ReleaseTags,
		InstallSuffix: ctx.InstallSuffix,
	}
}

func stringsAreSorted(a []string) bool {
	for i := 0; i < len(a)-1; i++ {
		if a[i] > a[i+1] {
			return false
		}
	}
	return true
}

// Use insertion sort since the slices we're sorting are small
// and it saves us an alloc from the sort.Interface conversion.
func insertionSortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func appendStrings(b []byte, scratch, a []string) ([]byte, []string) {
	if len(a) == 0 {
		return b, scratch
	}
	if !stringsAreSorted(a) {
		scratch = append(scratch[:0], a...)
		a = scratch
		insertionSortStrings(a)
	}
	b = append(b, a[0]...)
	for i := 1; i < len(a); i++ {
		b = append(b, ',')
		b = append(b, a[i]...)
	}
	return b, scratch
}

func appendBool(b []byte, val bool) []byte {
	if val {
		return append(b, "1|"...)
	}
	return append(b, "0|"...)
}

func ContextCacheKey(ctxt *build.Context) string {
	n := 11 + 2 // 11 fields and 2 bools

	n += len(ctxt.GOARCH) + len(ctxt.GOOS) + len(ctxt.GOROOT) + len(ctxt.GOPATH) +
		len(ctxt.Compiler) + len(ctxt.InstallSuffix) + len(ctxt.BuildTags) +
		len(ctxt.ToolTags) + len(ctxt.ReleaseTags) - 1

	for _, s := range ctxt.BuildTags {
		n += len(s)
	}
	for _, s := range ctxt.ToolTags {
		n += len(s)
	}
	for _, s := range ctxt.ReleaseTags {
		n += len(s)
	}

	b := make([]byte, 0, n)
	for _, s := range []string{
		ctxt.GOARCH,
		ctxt.GOOS,
		ctxt.GOROOT,
		ctxt.GOPATH,
		ctxt.Compiler,
		ctxt.InstallSuffix,
	} {
		b = append(b, s...)
		b = append(b, '|')
	}

	b = appendBool(b, ctxt.CgoEnabled)
	b = appendBool(b, ctxt.UseAllFiles)

	// scratch buffer for sorting
	var scratch []string

	// Append ReleaseTags first since they tend to be the longest
	b, scratch = appendStrings(b, scratch, ctxt.ReleaseTags)
	b = append(b, '|')
	b, scratch = appendStrings(b, scratch, ctxt.BuildTags)
	b = append(b, '|')
	b, scratch = appendStrings(b, scratch, ctxt.ToolTags)

	return *(*string)(unsafe.Pointer(&b))
}

var contextIDs struct {
	sync.RWMutex
	ids map[string]ContextKeyID
}

type ContextKeyID uint32

// ContextCacheKeyID returns a unique id for a build.Context cache key
// created with ContextCacheKey.
func ContextCacheKeyID(ctxtKey string) ContextKeyID {
	contextIDs.RLock()
	id, ok := contextIDs.ids[ctxtKey]
	contextIDs.RUnlock()
	if ok {
		return id
	}

	contextIDs.Lock()
	if contextIDs.ids == nil {
		contextIDs.ids = make(map[string]ContextKeyID)
	}
	id, ok = contextIDs.ids[ctxtKey]
	if !ok {
		id = ContextKeyID(len(contextIDs.ids)) + 1
		contextIDs.ids[ctxtKey] = id
	}
	contextIDs.Unlock()
	return id
}

/*
func ContextCacheKeySafe(ctxt *build.Context) string {
	var w strings.Builder
	n := 11 + 2 // 11 fields and 2 bools
	n += len(ctxt.GOARCH)
	n += len(ctxt.GOOS)
	n += len(ctxt.GOROOT)
	n += len(ctxt.GOPATH)
	n += len(ctxt.Compiler)
	n += len(ctxt.InstallSuffix)

	n += len(ctxt.BuildTags)
	for _, s := range ctxt.BuildTags {
		n += len(s)
	}
	n += len(ctxt.ToolTags)
	for _, s := range ctxt.ToolTags {
		n += len(s)
	}
	n += len(ctxt.ReleaseTags) - 1
	for _, s := range ctxt.ReleaseTags {
		n += len(s)
	}

	w.Grow(n)
	w.WriteString(ctxt.GOARCH)
	w.WriteByte('|')
	w.WriteString(ctxt.GOOS)
	w.WriteByte('|')
	w.WriteString(ctxt.GOROOT)
	w.WriteByte('|')
	w.WriteString(ctxt.GOPATH)
	w.WriteByte('|')
	w.WriteString(ctxt.Compiler)
	w.WriteByte('|')
	w.WriteString(ctxt.InstallSuffix)
	w.WriteByte('|')
	if ctxt.CgoEnabled {
		w.WriteByte('1') // only write if true
	}
	w.WriteByte('|')
	if ctxt.UseAllFiles {
		w.WriteByte('1') // only write if true
	}
	w.WriteByte('|')

	if len(ctxt.BuildTags) != 0 {
		for _, s := range ctxt.BuildTags {
			w.WriteString(s)
			w.WriteByte(',')
		}
		w.WriteByte('|')
	}
	if len(ctxt.ToolTags) != 0 {
		for _, s := range ctxt.ToolTags {
			w.WriteString(s)
			w.WriteByte(',')
		}
		w.WriteByte('|')
	}
	if len(ctxt.ReleaseTags) != 0 {
		for _, s := range ctxt.ReleaseTags {
			w.WriteString(s)
			w.WriteByte(',')
		}
	}

	// fmt.Printf("cap: %d len: %d delta: %d\n", w.Cap(), w.Len(), w.Cap()-w.Len())
	return w.String()
}
*/
