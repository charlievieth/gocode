package gocode

import (
	"go/scanner"
	"go/token"
	"sync"
)

// All the code in this file serves single purpose:
// It separates a function with the cursor inside and the rest of the code. I'm
// doing that, because sometimes parser is not able to recover itself from an
// error and the autocompletion results become less complete.

type tok_pos_pair struct {
	tok token.Token
	pos token.Pos
}

type tok_collection struct {
	tokens []tok_pos_pair
	fset   *token.FileSet
}

func (this *tok_collection) next(s *scanner.Scanner) bool {
	pos, tok, _ := s.Scan()
	if tok == token.EOF {
		return false
	}

	this.tokens = append(this.tokens, tok_pos_pair{tok, pos})
	return true
}

func (this *tok_collection) offset(p token.Pos) (pos int) {
	if f := this.fset.File(p); f != nil {
		pos = int(p) - f.Base()
	}
	return
}

func (this *tok_collection) find_decl_beg(pos int) int {
	lowest := 0
	lowpos := -1
	lowi := -1
	cur := 0
	for i := pos; i >= 0; i-- {
		t := this.tokens[i]
		switch t.tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}

		if cur < lowest {
			lowest = cur
			lowpos = this.offset(t.pos)
			lowi = i
		}
	}

	cur = lowest
	for i := lowi - 1; i >= 0; i-- {
		t := this.tokens[i]
		switch t.tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}
		if t.tok == token.SEMICOLON && cur == lowest {
			lowpos = this.offset(t.pos)
			break
		}
	}

	return lowpos
}

func (this *tok_collection) find_decl_end(pos int) int {
	highest := 0
	highpos := -1
	cur := 0

	if this.tokens[pos].tok == token.LBRACE {
		pos++
	}

	for i := pos; i < len(this.tokens); i++ {
		t := this.tokens[i]
		switch t.tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}

		if cur > highest {
			highest = cur
			highpos = this.offset(t.pos)
		}
	}

	return highpos
}

func (this *tok_collection) find_outermost_scope(cursor int) (int, int) {
	pos := 0

	for i, t := range this.tokens {
		if cursor <= this.offset(t.pos) {
			break
		}
		pos = i
	}

	return this.find_decl_beg(pos), this.find_decl_end(pos)
}

// return new cursor position, file without ripped part and the ripped part itself
// variants:
//   new-cursor, file-without-ripped-part, ripped-part
//   old-cursor, file, nil
func (this *tok_collection) rip_off_decl(file []byte, cursor int) (int, []byte, []byte) {
	this.fset = token.NewFileSet()
	var s scanner.Scanner
	s.Init(this.fset.AddFile("", this.fset.Base(), len(file)), file, nil, scanner.ScanComments)
	for this.next(&s) {
	}

	beg, end := this.find_outermost_scope(cursor)
	if beg == -1 || end == -1 {
		return cursor, file, nil
	}

	ripped := make([]byte, end+1-beg)
	copy(ripped, file[beg:end+1])

	newfile := make([]byte, len(file)-len(ripped))
	copy(newfile, file[:beg])
	copy(newfile[beg:], file[end+1:])

	return cursor - beg, newfile, ripped
}

func rip_off_decl(file []byte, cursor int) (int, []byte, []byte) {
	tc := tok_collection{tokens: get_token_slice()}
	pos, newfile, ripped := tc.rip_off_decl(file, cursor)
	put_token_slice(tc.tokens)
	return pos, newfile, ripped
}

var tokenPool struct {
	sync.Mutex
	pool [][]tok_pos_pair
}

func init() {
	for i := 0; i < 20; i++ {
		put_token_slice(make([]tok_pos_pair, 64))
	}
}

func get_token_slice() (p []tok_pos_pair) {
	tokenPool.Lock()
	if n := len(tokenPool.pool); n != 0 {
		p = tokenPool.pool[n-1][:0]
		tokenPool.pool = tokenPool.pool[:n-1]
	} else {
		p = make([]tok_pos_pair, 64)
	}
	tokenPool.Unlock()
	return
}

func put_token_slice(p []tok_pos_pair) {
	tokenPool.Lock()
	if len(tokenPool.pool) <= 20 {
		tokenPool.pool = append(tokenPool.pool, p)
	}
	tokenPool.Unlock()
}
