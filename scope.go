package gocode

//-------------------------------------------------------------------------
// scope
//-------------------------------------------------------------------------

type scope struct {
	// the package name that this scope resides in
	pkgname  string
	parent   *scope // nil for universe scope
	entities map[string]*decl
}

func new_named_scope(outer *scope, name string) *scope {
	s := new_scope(outer)
	s.pkgname = name
	return s
}

func new_scope_size(outer *scope, size int) *scope {
	var pkgname string
	if outer != nil {
		pkgname = outer.pkgname
	}
	return &scope{
		pkgname:  pkgname,
		parent:   outer,
		entities: make(map[string]*decl, size),
	}
}

func new_scope(outer *scope) *scope {
	return new_scope_size(outer, 0)
}

// returns: new, prev
func advance_scope(s *scope) (*scope, *scope) {
	if len(s.entities) == 0 {
		return s, s.parent
	}
	return new_scope(s), s
}

// adds declaration or returns an existing one
func (s *scope) add_named_decl(d *decl) *decl {
	return s.add_decl(d.name, d)
}

func (s *scope) add_decl(name string, d *decl) *decl {
	decl, ok := s.entities[name]
	if !ok {
		s.entities[name] = d
		return d
	}
	return decl
}

func (s *scope) replace_decl(name string, d *decl) {
	s.entities[name] = d
}

func (s *scope) merge_decl(d *decl) {
	decl, ok := s.entities[d.name]
	if !ok {
		s.entities[d.name] = d
	} else {
		decl := decl.deep_copy()
		decl.expand_or_replace(d)
		s.entities[d.name] = decl
	}
}

func (s *scope) lookup(name string) *decl {
	decl, ok := s.entities[name]
	if !ok {
		if s.parent != nil {
			return s.parent.lookup(name)
		} else {
			return nil
		}
	}
	return decl
}
