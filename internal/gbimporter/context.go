package gbimporter

import "go/build"

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
		ReleaseTags:   ctx.ReleaseTags,
		InstallSuffix: ctx.InstallSuffix,
	}
}

// Update context ctxt to match PackedContext
func (p *PackedContext) Update(ctxt *build.Context) {
	ctxt.GOARCH = p.GOARCH
	ctxt.GOOS = p.GOOS
	ctxt.GOROOT = p.GOROOT
	ctxt.GOPATH = p.GOPATH
	ctxt.CgoEnabled = p.CgoEnabled
	ctxt.UseAllFiles = p.UseAllFiles
	ctxt.Compiler = p.Compiler
	ctxt.BuildTags = p.BuildTags
	ctxt.ReleaseTags = p.ReleaseTags
	ctxt.InstallSuffix = p.InstallSuffix
}
