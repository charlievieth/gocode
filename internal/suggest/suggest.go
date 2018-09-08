package suggest

import (
	"bytes"
	"errors"
	"go/ast"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/mdempsky/gocode/internal/buildutil"
	"github.com/mdempsky/gocode/internal/context"
	"github.com/mdempsky/gocode/internal/lookdot"
)

type Config struct {
	Importer types.Importer
	Context  *context.Packed
	Logf     func(fmt string, args ...interface{})
	Builtin  bool
}

// Suggest returns a list of suggestion candidates and the length of
// the text that should be replaced, if any.
func (c *Config) Suggest(filename string, data []byte, cursor int) ([]Candidate, int) {
	if cursor < 0 {
		return nil, 0
	}

	fset, pos, pkg := c.analyzePackage(filename, data, cursor)
	if pkg == nil {
		return nil, 0
	}
	scope := pkg.Scope().Innermost(pos)

	ctx, expr, partial := deduceCursorContext(data, cursor)
	b := candidateCollector{
		localpkg: pkg,
		partial:  partial,
		filter:   objectFilters[partial],
		builtin:  ctx != selectContext && c.Builtin,
	}

	switch ctx {
	case selectContext:
		tv, _ := types.Eval(fset, pkg, pos, expr)
		if lookdot.Walk(&tv, b.appendObject) {
			break
		}

		_, obj := scope.LookupParent(expr, pos)
		if pkgName, isPkg := obj.(*types.PkgName); isPkg {
			c.packageCandidates(pkgName.Imported(), &b)
			break
		}

		return nil, 0

	case compositeLiteralContext:
		tv, _ := types.Eval(fset, pkg, pos, expr)
		if tv.IsType() {
			if _, isStruct := tv.Type.Underlying().(*types.Struct); isStruct {
				c.fieldNameCandidates(tv.Type, &b)
				break
			}
		}

		fallthrough
	default:
		c.scopeCandidates(scope, pos, &b)
	}

	res := b.getCandidates()
	if len(res) == 0 {
		return nil, 0
	}
	return res, len(partial)
}

func (c *Config) analyzePackage(filename string, data []byte, cursor int) (*token.FileSet, token.Pos, *types.Package) {
	// If we're in trailing white space at the end of a scope,
	// sometimes go/types doesn't recognize that variables should
	// still be in scope there.
	filesemi := bytes.Join([][]byte{data[:cursor], []byte(";"), data[cursor:]}, nil)

	fset := token.NewFileSet()
	fileAST, err := parser.ParseFile(fset, filename, filesemi, parser.AllErrors)
	if err != nil {
		c.logParseError("Error parsing input file (outer block)", err)
	}
	astPos := fileAST.Pos()
	if astPos == 0 {
		return nil, token.NoPos, nil
	}
	pos := fset.File(astPos).Pos(cursor)

	files, err := c.parseOtherPackageFiles(fset, filename, fileAST.Name.Name)
	if err != nil && len(files) == 0 {
		c.logParseError("Error parsing other file", err)
	}
	files = append(files, fileAST)

	// Clear any function bodies other than where the cursor
	// is. They're not relevant to suggestions and only slow down
	// typechecking.
	for _, file := range files {
		for _, decl := range file.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && (pos < fd.Pos() || pos >= fd.End()) {
				fd.Body = nil
			}
		}
	}

	cfg := types.Config{
		Importer: c.Importer,
		Error:    func(err error) {},
	}
	pkg, _ := cfg.Check("", fset, files, nil)

	return fset, pos, pkg
}

func (c *Config) fieldNameCandidates(typ types.Type, b *candidateCollector) {
	s := typ.Underlying().(*types.Struct)
	for i, n := 0, s.NumFields(); i < n; i++ {
		b.appendObject(s.Field(i))
	}
}

func (c *Config) packageCandidates(pkg *types.Package, b *candidateCollector) {
	c.scopeCandidates(pkg.Scope(), token.NoPos, b)
}

func (c *Config) scopeCandidates(scope *types.Scope, pos token.Pos, b *candidateCollector) {
	seen := make(map[string]bool)
	for scope != nil {
		for _, name := range scope.Names() {
			if seen[name] {
				continue
			}
			seen[name] = true
			_, obj := scope.LookupParent(name, pos)
			if obj != nil {
				b.appendObject(obj)
			}
		}
		scope = scope.Parent()
	}
}

func (c *Config) logParseError(intro string, err error) {
	if c.Logf == nil {
		return
	}
	if el, ok := err.(scanner.ErrorList); ok {
		c.Logf("%s:", intro)
		for _, er := range el {
			c.Logf(" %s", er)
		}
	} else {
		c.Logf("%s: %s", intro, err)
	}
}

func readfilenames(dirname string) ([]string, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil && len(names) == 0 {
		return nil, err
	}
	return names, nil
}

// parseGate controls the number of concurrent file parses.
// Send before an operation and receive after.
var parseGate = make(chan struct{}, runtime.NumCPU()*2)

func (c *Config) parseOtherPackageFiles(fset *token.FileSet, filename, pkgName string) ([]*ast.File, error) {
	if filename == "" {
		return nil, errors.New("empty filename")
	}

	dir, file := filepath.Split(filename)
	names, err := readfilenames(dir)
	if err != nil {
		return nil, err
	}
	isTestFile := strings.HasSuffix(file, "_test.go")

	// create a copy of build.Default and update it
	ctxt := build.Default
	c.Context.Update(&ctxt)

	var (
		out   []*ast.File
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	for _, name := range names {
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if name == file || !strings.HasSuffix(name, ".go") {
			continue
		}
		if !isTestFile && strings.HasSuffix(name, "_test.go") {
			continue
		}

		wg.Add(1)
		parseGate <- struct{}{}
		go func(path string) {
			defer func() {
				wg.Done()
				<-parseGate
			}()
			pkg, ok := buildutil.ShouldBuild(&ctxt, path)
			if !ok || pkg != pkgName {
				return
			}
			af, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil && af == nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			out = append(out, af)
			mu.Unlock()
		}(filepath.Join(dir, name))
	}
	wg.Wait()

	return out, first
}
