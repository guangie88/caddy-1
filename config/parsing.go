package config

// This file contains the recursive-descent parsing
// functions.

// begin is the top of the recursive-descent parsing.
// It parses at most one server configuration (an address
// and its directives).
func (p *parser) begin() error {
	err := p.address()
	if err != nil {
		return err
	}

	err = p.addressBlock()
	if err != nil {
		return err
	}

	return nil
}

// address expects that the current token is a host:port
// combination.
func (p *parser) address() error {
	if p.tkn() == "}" || p.tkn() == "{" {
		return p.err("Syntax", "'"+p.tkn()+"' is not EOF or address")
	}
	p.cfg.Host, p.cfg.Port = parseAddress(p.tkn())
	return nil
}

// addressBlock leads into parsing directives, including
// possible opening/closing curly braces around the block.
// It handles directives enclosed by curly braces and
// directives not enclosed by curly braces.
func (p *parser) addressBlock() error {
	if !p.next() {
		// file consisted of only an address
		return nil
	}

	err := p.openCurlyBrace()
	if err != nil {
		// meh, single-server configs don't need curly braces
		p.unused = true // we read the token but aren't consuming it
		return p.directives()
	}

	err = p.directives()
	if err != nil {
		return err
	}

	err = p.closeCurlyBrace()
	if err != nil {
		return err
	}
	return nil
}

// openCurlyBrace expects the current token to be an
// opening curly brace. This acts like an assertion
// because it returns an error if the token is not
// a opening curly brace. It does not advance the token.
func (p *parser) openCurlyBrace() error {
	if p.tkn() != "{" {
		return p.syntaxErr("{")
	}
	return nil
}

// closeCurlyBrace expects the current token to be
// a closing curly brace. This acts like an assertion
// because it returns an error if the token is not
// a closing curly brace. It does not advance the token.
func (p *parser) closeCurlyBrace() error {
	if p.tkn() != "}" {
		return p.syntaxErr("}")
	}
	return nil
}

// directives parses through all the directives
// and it expects the current token to be the first
// directive. It goes until EOF or closing curly
// brace which ends the address block.
func (p *parser) directives() error {
	for p.next() {
		if p.tkn() == "}" {
			// end of address scope
			break
		}
		if p.tkn()[0] == '/' {
			// Path scope (a.k.a. location context)
			// TODO: The parser can handle the syntax (obviously), but the
			// implementation is incomplete. This is intentional,
			// until we can better decide what kind of feature set we
			// want to support. Until this is ready, we leave this
			// syntax undocumented.

			// location := p.tkn()

			if !p.next() {
				return p.eofErr()
			}

			err := p.openCurlyBrace()
			if err != nil {
				return err
			}

			for p.next() {
				err := p.closeCurlyBrace()
				if err == nil { // end of location context
					break
				}

				// TODO: How should we give the context to the directives?
				// Or how do we tell the server that these directives should only
				// be executed for requests routed to the current path?

				err = p.directive()
				if err != nil {
					return err
				}
			}
		} else if err := p.directive(); err != nil {
			return err
		}
	}
	return nil
}

// directive asserts that the current token is either a built-in
// directive or a registered middleware directive; otherwise an error
// will be returned.
func (p *parser) directive() error {
	if fn, ok := validDirectives[p.tkn()]; ok {
		// Built-in (standard) directive
		err := fn(p)
		if err != nil {
			return err
		}
	} else if middlewareRegistered(p.tkn()) {
		// Middleware directive
		err := p.collectTokens()
		if err != nil {
			return err
		}
	} else {
		return p.err("Syntax", "Unexpected token '"+p.tkn()+"', expecting a valid directive")
	}
	return nil
}

// collectTokens consumes tokens until the directive's scope
// closes (either end of line or end of curly brace block).
// It creates a controller which is stored in the parser for
// later use by the middleware.
func (p *parser) collectTokens() error {
	directive := p.tkn()
	line := p.line()
	nesting := 0
	breakOk := false
	cont := newController(p)

	// Re-use a duplicate directive's controller from before
	// (the parsing logic in the middleware generator must
	// account for multiple occurrences of its directive, even
	// if that means returning an error or overwriting settings)
	if existing, ok := p.other[directive]; ok {
		cont = existing
	}

	// The directive is appended as a relevant token
	cont.tokens = append(cont.tokens, p.lexer.token)

	for p.next() {
		if p.tkn() == "{" {
			nesting++
		} else if p.line() > line && nesting == 0 {
			p.unused = true
			breakOk = true
			break
		} else if p.tkn() == "}" && nesting > 0 {
			nesting--
		} else if p.tkn() == "}" && nesting == 0 {
			return p.err("Syntax", "Unexpected '}' because no matching open curly brace '{'")
		}
		cont.tokens = append(cont.tokens, p.lexer.token)
	}

	if !breakOk || nesting > 0 {
		return p.eofErr()
	}

	p.other[directive] = cont
	return nil
}
