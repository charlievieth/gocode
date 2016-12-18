package gocode

import "sync"

//-------------------------------------------------------------------------
// scope
//-------------------------------------------------------------------------

type scope struct {
	parent   *scope // nil for universe scope
	entities map[string]*decl
	mu       sync.RWMutex
}

func new_scope(outer *scope) *scope {
	s := new(scope)
	s.parent = outer
	s.entities = make(map[string]*decl)
	return s
}

// returns: new, prev
func advance_scope(s *scope) (next *scope, prev *scope) {
	s.mu.RLock()
	if len(s.entities) == 0 {
		next = s
		prev = s.parent
	} else {
		next = new_scope(s)
		prev = s
	}
	s.mu.RUnlock()
	return
}

// adds declaration or returns an existing one
func (s *scope) add_named_decl(d *decl) *decl {
	return s.add_decl(d.name, d)
}

func (s *scope) find_decl(name string) (*decl, bool) {
	s.mu.RLock()
	d, ok := s.entities[name]
	s.mu.RUnlock()
	return d, ok
}

func (s *scope) add_decl(name string, d *decl) *decl {
	if dd, ok := s.find_decl(name); ok {
		return dd
	}
	s.mu.Lock()
	dd, ok := s.entities[name]
	if !ok {
		dd = d
		s.entities[name] = dd
	}
	s.mu.Unlock()
	return dd
}

func (s *scope) replace_decl(name string, d *decl) {
	s.mu.Lock()
	s.entities[name] = d
	s.mu.Unlock()
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

func (s *scope) Parent() *scope {
	s.mu.RLock()
	p := s.parent
	s.mu.RUnlock()
	return p
}

func (s *scope) lookup(name string) *decl {
	s.mu.RLock()
	d, ok := s.entities[name]
	p := s.parent
	s.mu.RUnlock()
	if ok || p == nil {
		return d
	}
	return p.lookup(name)
}
