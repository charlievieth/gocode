package gocode

import (
	"bytes"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charlievieth/buildutil"
)

//-------------------------------------------------------------------------
// out_buffers
//
// Temporary structure for writing autocomplete response.
//-------------------------------------------------------------------------

// fields must be exported for RPC
type candidate struct {
	Name    string
	Type    string
	Class   decl_class
	Package string
}

type out_buffers struct {
	tmpbuf            *bytes.Buffer
	candidates        []candidate
	canonical_aliases map[string]string
	ctx               *auto_complete_context
	tmpns             map[string]bool
	ignorecase        bool
}

func new_out_buffers(ctx *auto_complete_context) *out_buffers {
	b := new(out_buffers)
	b.tmpbuf = bytes.NewBuffer(make([]byte, 0, 1024))
	b.candidates = make([]candidate, 0, 64)
	b.ctx = ctx
	b.canonical_aliases = make(map[string]string, len(b.ctx.current.packages))
	for _, imp := range b.ctx.current.packages {
		b.canonical_aliases[imp.abspath] = imp.alias
	}
	return b
}

func (b *out_buffers) Len() int {
	return len(b.candidates)
}

func (b *out_buffers) Less(i, j int) bool {
	x := b.candidates[i]
	y := b.candidates[j]
	if x.Class == y.Class {
		return x.Name < y.Name
	}
	return x.Class < y.Class
}

func (b *out_buffers) Swap(i, j int) {
	b.candidates[i], b.candidates[j] = b.candidates[j], b.candidates[i]
}

func (b *out_buffers) append_decl(p, name, pkg string, decl *decl, class decl_class) {
	c1 := !g_config.ProposeBuiltins() && decl.scope == g_universe_scope && decl.name != "Error"
	c2 := class != decl_invalid && decl.class != class
	c3 := class == decl_invalid && !has_prefix(name, p, b.ignorecase)
	c4 := !decl.matches()
	c5 := !check_type_expr(decl.typ)

	if c1 || c2 || c3 || c4 || c5 {
		return
	}

	decl.pretty_print_type(b.tmpbuf, b.canonical_aliases)
	b.candidates = append(b.candidates, candidate{
		Name:    name,
		Type:    b.tmpbuf.String(),
		Class:   decl.class,
		Package: pkg,
	})
	b.tmpbuf.Reset()
}

func (b *out_buffers) append_embedded(p string, decl *decl, pkg string, class decl_class) {
	if decl.embedded == nil {
		return
	}

	first_level := false
	if b.tmpns == nil {
		// first level, create tmp namespace
		b.tmpns = make(map[string]bool, len(decl.children))
		first_level = true

		// add all children of the current decl to the namespace
		for _, c := range decl.children {
			b.tmpns[c.name] = true
		}
	}

	for _, emb := range decl.embedded {
		typedecl := type_to_decl(emb, decl.scope)
		if typedecl == nil {
			continue
		}

		// prevent infinite recursion here
		if typedecl.is_visited() {
			continue
		}
		typedecl.set_visited()
		defer typedecl.clear_visited()

		for _, c := range typedecl.children {
			if _, has := b.tmpns[c.name]; has {
				continue
			}
			b.append_decl(p, c.name, pkg, c, class)
			b.tmpns[c.name] = true
		}
		b.append_embedded(p, typedecl, pkg, class)
	}

	if first_level {
		// remove tmp namespace
		b.tmpns = nil
	}
}

//-------------------------------------------------------------------------
// auto_complete_context
//
// Context that holds cache structures for autocompletion needs. It
// includes cache for packages and for main package files.
//-------------------------------------------------------------------------

type auto_complete_context struct {
	current *auto_complete_file // currently edited file
	others  []*decl_file_cache  // other files of the current package
	pkg     *scope

	pcache    package_cache // packages cache
	declcache *decl_cache   // top-level declarations cache
}

func new_auto_complete_context(pcache package_cache, declcache *decl_cache) *auto_complete_context {
	c := new(auto_complete_context)
	c.current = new_auto_complete_file("", declcache.context)
	c.pcache = pcache
	c.declcache = declcache
	return c
}

func (c *auto_complete_context) update_caches() {
	// temporary map for packages that we need to check for a cache expiration
	// map is used as a set of unique items to prevent double checks
	ps := make(map[string]*package_file_cache, len(c.current.packages))

	// collect import information from all of the files
	c.pcache.append_packages(ps, c.current.packages)
	c.others = get_other_package_files(c.current.name, c.current.package_name, c.declcache)
	for _, other := range c.others {
		c.pcache.append_packages(ps, other.packages)
	}

	update_packages(ps)

	// fix imports for all files
	fixup_packages(c.current.filescope, c.current.packages, c.pcache)
	for _, f := range c.others {
		fixup_packages(f.filescope, f.packages, c.pcache)
	}

	// At this point we have collected all top level declarations, now we need to
	// merge them in the common package block.
	c.merge_decls()
}

func (c *auto_complete_context) merge_decls() {
	// rough estimate of the cache size
	n := len(c.current.decls)
	for _, f := range c.others {
		n += len(f.decls)
	}
	c.pkg = new_scope_size(g_universe_scope, n)

	merge_decls(c.current.filescope, c.pkg, c.current.decls)
	merge_decls_from_packages(c.pkg, c.current.packages, c.pcache)
	for _, f := range c.others {
		merge_decls(f.filescope, c.pkg, f.decls)
		merge_decls_from_packages(c.pkg, f.packages, c.pcache)
	}

	// special pass for type aliases which also have methods, while this is
	// valid code, it shouldn't happen a lot in practice, so, whatever
	// let's move all type alias methods to their first non-alias type down in
	// the chain
	propagate_type_alias_methods(c.pkg)
}

func (c *auto_complete_context) make_decl_set(scope *scope) map[string]*decl {
	set := make(map[string]*decl, len(c.pkg.entities)*2)
	make_decl_set_recursive(set, scope)
	return set
}

func (c *auto_complete_context) get_candidates_from_set(set map[string]*decl, partial string, class decl_class, b *out_buffers) {
	for key, value := range set {
		if value == nil {
			continue
		}
		value.infer_type()
		pkgname := ""
		if pkg, ok := c.pcache[value.name]; ok {
			pkgname = pkg.import_name
		}
		b.append_decl(partial, key, pkgname, value, class)
	}
}

func (c *auto_complete_context) get_candidates_from_decl_alias(cc cursor_context, class decl_class, b *out_buffers) {
	if cc.decl.is_visited() {
		return
	}

	cc.decl = cc.decl.type_dealias()
	if cc.decl == nil {
		return
	}

	cc.decl.set_visited()
	defer cc.decl.clear_visited()

	c.get_candidates_from_decl(cc, class, b)
	return
}

func (c *auto_complete_context) decl_package_import_path(decl *decl) string {
	if decl == nil || decl.scope == nil {
		return ""
	}
	if pkg, ok := c.pcache[decl.scope.pkgname]; ok {
		return pkg.import_name
	}
	return ""
}

func (c *auto_complete_context) get_candidates_from_decl(cc cursor_context, class decl_class, b *out_buffers) {
	if cc.decl.is_alias() {
		c.get_candidates_from_decl_alias(cc, class, b)
		return
	}

	// propose all children of a subject declaration and
	for _, decl := range cc.decl.children {
		if cc.decl.class == decl_package && !ast.IsExported(decl.name) {
			continue
		}
		if cc.struct_field {
			// if we're autocompleting struct field init, skip all methods
			if _, ok := decl.typ.(*ast.FuncType); ok {
				continue
			}
		}
		b.append_decl(cc.partial, decl.name, c.decl_package_import_path(decl), decl, class)
	}
	// propose all children of an underlying struct/interface type
	adecl := advance_to_struct_or_interface(cc.decl)
	if adecl != nil && adecl != cc.decl {
		for _, decl := range adecl.children {
			if decl.class == decl_var {
				b.append_decl(cc.partial, decl.name, c.decl_package_import_path(decl), decl, class)
			}
		}
	}
	// propose all children of its embedded types
	b.append_embedded(cc.partial, cc.decl, c.decl_package_import_path(cc.decl), class)
}

func (c *auto_complete_context) get_import_candidates(partial string, b *out_buffers) {
	currentPackagePath, pkgdirs := gocodeDaemon.context.pkg_dirs()
	resultSet := map[string]struct{}{}
	for _, pkgdir := range pkgdirs {
		// convert srcpath to pkgpath and get candidates
		get_import_candidates_dir(pkgdir, filepath.FromSlash(partial), b.ignorecase, currentPackagePath, resultSet)
	}
	for k := range resultSet {
		b.candidates = append(b.candidates, candidate{Name: k, Class: decl_import})
	}
}

func get_import_candidates_dir(root, partial string, ignorecase bool, currentPackagePath string, r map[string]struct{}) {
	var fpath string
	var match bool
	if strings.HasSuffix(partial, "/") {
		fpath = filepath.Join(root, partial)
	} else {
		fpath = filepath.Join(root, filepath.Dir(partial))
		match = true
	}
	fi := readdir(fpath)
	for i := range fi {
		name := fi[i].Name()
		rel, err := filepath.Rel(root, filepath.Join(fpath, name))
		if err != nil {
			panic(err)
		}
		if match && !has_prefix(rel, partial, ignorecase) {
			continue
		} else if fi[i].IsDir() {
			get_import_candidates_dir(root, rel+string(filepath.Separator), ignorecase, currentPackagePath, r)
		} else {
			ext := filepath.Ext(name)
			if ext != ".a" {
				continue
			} else {
				rel = rel[0 : len(rel)-2]
			}
			if ipath, ok := vendorlessImportPath(filepath.ToSlash(rel), currentPackagePath); ok {
				r[ipath] = struct{}{}
			}
		}
	}
}

// returns three slices of the same length containing:
// 1. apropos names
// 2. apropos types (pretty-printed)
// 3. apropos classes
// and length of the part that should be replaced (if any)
func (c *auto_complete_context) apropos(file []byte, filename string, cursor int) ([]candidate, int) {
	c.current.cursor = cursor
	c.current.name = filename

	// Update caches and parse the current file.
	// This process is quite complicated, because I was trying to design it in a
	// concurrent fashion. Apparently I'm not really good at that. Hopefully
	// will be better in future.

	// Ugly hack, but it actually may help in some cases. Insert a
	// semicolon right at the cursor location.
	filesemi := make([]byte, len(file)+1)
	copy(filesemi, file[:cursor])
	filesemi[cursor] = ';'
	copy(filesemi[cursor+1:], file[cursor:])

	// Does full processing of the currently edited file (top-level declarations plus
	// active function).
	c.current.process_data(filesemi)

	// Updates cache of other files and packages. See the function for details of
	// the process. At the end merges all the top-level declarations into the package
	// block.
	c.update_caches()

	// And we're ready to Go. ;)

	b := new_out_buffers(c)

	partial := 0
	cc, ok := c.deduce_cursor_context(file, cursor)
	if !ok {
		var d *decl
		if ident, ok := cc.expr.(*ast.Ident); ok && g_config.UnimportedPackages() {
			d = resolveKnownPackageIdent(ident.Name, c.current.name, c.current.context)
		}
		if d == nil {
			return nil, 0
		}
		cc.decl = d
	}

	class := decl_invalid
	switch cc.partial {
	case "const":
		class = decl_const
	case "var":
		class = decl_var
	case "type":
		class = decl_type
	case "func":
		class = decl_func
	case "package":
		class = decl_package
	}

	if cc.decl_import {
		c.get_import_candidates(cc.partial, b)
		if cc.partial != "" && len(b.candidates) == 0 {
			// as a fallback, try case insensitive approach
			b.ignorecase = true
			c.get_import_candidates(cc.partial, b)
		}
	} else if cc.decl == nil {
		// In case if no declaraion is a subject of completion, propose all:
		set := c.make_decl_set(c.current.scope)
		c.get_candidates_from_set(set, cc.partial, class, b)
		if cc.partial != "" && len(b.candidates) == 0 {
			// as a fallback, try case insensitive approach
			b.ignorecase = true
			c.get_candidates_from_set(set, cc.partial, class, b)
		}
	} else {
		c.get_candidates_from_decl(cc, class, b)
		if cc.partial != "" && len(b.candidates) == 0 {
			// as a fallback, try case insensitive approach
			b.ignorecase = true
			c.get_candidates_from_decl(cc, class, b)
		}
	}
	partial = len(cc.partial)

	if len(b.candidates) == 0 {
		return nil, 0
	}

	sort.Sort(b)
	return b.candidates, partial
}

func update_packages(ps map[string]*package_file_cache) {
	// initiate package cache update
	var wg sync.WaitGroup
	var failed int32

	for _, p := range ps {
		wg.Add(1)
		go func(p *package_file_cache) {
			defer func() {
				wg.Done()
				if err := recover(); err != nil {
					print_backtrace(err)
					atomic.StoreInt32(&failed, 1)
				}
			}()
			p.update_cache()
		}(p)
	}

	// wait for its completion
	wg.Wait()
	if atomic.LoadInt32(&failed) != 0 {
		panic("One of the package cache updaters panicked")
	}
}

func collect_type_alias_methods(d *decl) map[string]*decl {
	if d == nil || d.is_visited() || !d.is_alias() {
		return nil
	}
	d.set_visited()
	defer d.clear_visited()

	// add own methods
	m := map[string]*decl{}
	for k, v := range d.children {
		m[k] = v
	}

	// recurse into more aliases
	dd := type_to_decl(d.typ, d.scope)
	for k, v := range collect_type_alias_methods(dd) {
		m[k] = v
	}

	return m
}

func propagate_type_alias_methods(s *scope) {
	for _, e := range s.entities {
		if !e.is_alias() {
			continue
		}

		methods := collect_type_alias_methods(e)
		if len(methods) == 0 {
			continue
		}

		dd := e.type_dealias()
		if dd == nil {
			continue
		}

		decl := dd.deep_copy()
		for _, v := range methods {
			decl.add_child(v)
		}
		s.entities[decl.name] = decl
	}
}

func merge_decls(filescope *scope, pkg *scope, decls map[string]*decl) {
	for _, d := range decls {
		pkg.merge_decl(d)
	}
	filescope.parent = pkg
}

func merge_decls_from_packages(pkgscope *scope, pkgs []package_import, pcache package_cache) {
	for _, p := range pkgs {
		path, alias := p.abspath, p.alias
		if alias != "." {
			continue
		}
		p := pcache[path].main
		if p == nil {
			continue
		}
		for _, d := range p.children {
			if ast.IsExported(d.name) {
				pkgscope.merge_decl(d)
			}
		}
	}
}

func fixup_packages(filescope *scope, pkgs []package_import, pcache package_cache) {
	for _, p := range pkgs {
		path, alias := p.abspath, p.alias
		if alias == "" {
			alias = pcache[path].defalias
		}
		// skip packages that will be merged to the package scope
		if alias == "." {
			continue
		}
		filescope.replace_decl(alias, pcache[path].main)
	}
}

func get_other_package_files(filename, packageName string, declcache *decl_cache) []*decl_file_cache {
	others := find_other_package_files(filename, packageName)
	ret := make([]*decl_file_cache, len(others))

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed int32
	)
	for i, nm := range others {
		wg.Add(1)
		go func(i int, name string) {
			defer func() {
				wg.Done()
				if err := recover(); err != nil {
					print_backtrace(err)
					atomic.StoreInt32(&failed, 1)
				}
			}()

			dc := declcache.get_and_update(name)
			mu.Lock()
			ret[i] = dc
			mu.Unlock()
		}(i, nm)
	}
	wg.Wait()

	if atomic.LoadInt32(&failed) != 0 {
		panic("One of the decl cache updaters panicked")
	}
	return ret
}

func find_other_package_files(filename, package_name string) []string {
	if filename == "" {
		return nil
	}

	dir, file := filepath.Split(filename)
	files_in_dir, err := readdir_gofiles_lstat(dir)
	if err != nil {
		// TODO (CEV): panic seems a little aggressive
		// and will blow out the cache on restart
		panic(err)
	}

	const non_regular = os.ModeDir | os.ModeSymlink |
		os.ModeDevice | os.ModeNamedPipe | os.ModeSocket

	out := make([]string, 0, len(files_in_dir))
	for _, stat := range files_in_dir {
		name := stat.Name()
		if !has_go_ext(name) || name == file || stat.Mode()&non_regular != 0 {
			continue
		}
		abspath := dir + string(filepath.Separator) + name
		if file_package_name(abspath) == package_name {
			out = append(out, abspath)
		}
	}

	return out
}

func file_package_name(filename string) string {
	name, _ := buildutil.ReadPackageName(filename, nil)
	return name
}

func make_decl_set_recursive(set map[string]*decl, scope *scope) {
	for name, ent := range scope.entities {
		if _, ok := set[name]; !ok {
			set[name] = ent
		}
	}
	if scope.parent != nil {
		make_decl_set_recursive(set, scope.parent)
	}
}

func check_func_field_list(f *ast.FieldList) bool {
	if f == nil {
		return true
	}

	for _, field := range f.List {
		if !check_type_expr(field.Type) {
			return false
		}
	}
	return true
}

// checks for a type expression correctness, it the type expression has
// ast.BadExpr somewhere, returns false, otherwise true
func check_type_expr(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.StarExpr:
		return check_type_expr(t.X)
	case *ast.ArrayType:
		return check_type_expr(t.Elt)
	case *ast.SelectorExpr:
		return check_type_expr(t.X)
	case *ast.FuncType:
		a := check_func_field_list(t.Params)
		b := check_func_field_list(t.Results)
		return a && b
	case *ast.MapType:
		a := check_type_expr(t.Key)
		b := check_type_expr(t.Value)
		return a && b
	case *ast.Ellipsis:
		return check_type_expr(t.Elt)
	case *ast.ChanType:
		return check_type_expr(t.Value)
	case *ast.BadExpr:
		return false
	default:
		return true
	}
	return true
}
