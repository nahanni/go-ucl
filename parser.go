/*
 * Copyright (c) 2015 Leon Dang, Nahanni Systems Inc
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 *
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer
 *    in this position and unchanged.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS "AS IS" AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */

package ucl

import (
	"fmt"
	"io"
)

// The order of the keys as they appear in the file; this allows the user to
// have their own order for items.
const KeyOrder = "--ucl-keyorder--"

// Allow to disable constructing the KeyOrder arrays
var UclExportKeyOrder bool = true

var Ucldebug bool = true
func debug(a... interface{}) {
	if Ucldebug {
		fmt.Println(a)
	}
}


type Parser struct {
	scanner *scanner

	ucl     map[string] interface{}

	tags    []*tag
	tagsi   int

	done    bool
	err     error
}

func NewParser(r io.Reader) *Parser {
	p := &Parser{
		scanner: newScanner(r),
		ucl: make(map[string] interface{}),
	}

	return p
}

func (p *Parser) nexttag() (*tag, error) {
	var err error

	if p.done {
		return nil, io.EOF
	}

	for {
		if p.tagsi >= len(p.tags) {
			p.tags, err = p.scanner.nexttags()
			if err != nil {
				return nil, err
			}
			p.tagsi = 0
		}
		for ; p.tagsi < len(p.tags); p.tagsi++ {
			m := p.tags[p.tagsi]
			if m.state == WHITESPACE || m.state == LCOMMENT ||
			   m.state == HCOMMENT {
				continue
			}
			p.tagsi++

			return m, nil
		}
	}

	return nil, fmt.Errorf("end of input line %d", p.scanner.line)
}


func (p *Parser) parsevalue(t *tag, parent interface{}) (interface{}, error) {
	var err error

restart:
	if t == nil {
		t, err = p.nexttag()
		if err != nil {
			return nil, err
		}
	}

	switch t.state {
	case TAG, QUOTE, VQUOTE, SLASH:
		// this could be either a value or a new key
		// have to see if the followon tags exist
		nt, err := p.nexttag()
		if err != nil {
			return nil, err
		}

		if nt == nil || nt.state == SEMICOL || nt.state == COMMA {
			return string(t.val), nil;  // leaf value; done
		}
		if nt.state == BRACECLOSE || nt.state == BRACKETCLOSE {
			nt.val = t.val
			return nt, nil
		}

		// "t" is a new key tag
		themap := make(map[string] interface{})
		res, err := p.parsevalue(nt, parent)

		if err != nil {
			debug("Error:", err)
			return nil, err
		}

		korder := make([]string, 1, 16)
		korder[0] = string(t.val)
		themap[KeyOrder] = korder
		themap[string(t.val)] = res
		return themap, nil

	case SEMICOL:
		// no value, let parent handle it
		if parent == nil {
			return t, fmt.Errorf("unexpected ';' at line %d", p.scanner.line)
		}
		return parent, nil

	case COMMA:
		// no value, let parent handle it
		if parent == nil {
			return t, fmt.Errorf("unexpected ',' at line %d", p.scanner.line)
		}
		return parent, nil

	case MLSTRING:
		// this must only be a value
		return string(t.val), nil

	case BRACEOPEN:
		// {, new map
		res, err := p.parse(t, parent)
		if err != nil {
			debug("parse error:", err)
		}
		return res, err

	case BRACECLOSE:
		// return until we hit the stack that has BRACEOPEN
		return parent, nil

	case BRACKETOPEN:
		thelist := make([]interface{}, 0, 32)
		res, err := p.parselist(nil, thelist)
		return res, err

	case BRACKETCLOSE:
		// list finished
		return parent, nil

	case EQUAL, COLON:
		t = nil
		goto restart
	}

	return nil, nil
}

func (p *Parser) parselist(t *tag, parent []interface{}) (ret interface{}, err error) {
	// Parse until bracket close
restart:
	if t == nil {
		t, err = p.nexttag()
		if err != nil {
			return nil, err
		}
	}

	switch t.state {
	case BRACKETCLOSE:
		// list finished
		return parent, nil

	case SEMICOL, COLON, EQUAL:
		// no value, let parent handle it
		return nil, fmt.Errorf("Invalid tag %s line %d",
		                       string(t.val), p.scanner.line)
	case COMMA:
		t = nil
		goto restart

	default:
		// append child
		res, err := p.parsevalue(t, nil)
		if err != nil {
			debug("error parsing value:", err)
			return nil, err
		} else {
			if restag, ok := res.(*tag); ok {
				// result is a tag; parsevalue didn't handle it
				if restag.state == BRACKETCLOSE {
					parent = append(parent, string(restag.val))
					return parent, nil
				} else {
					return nil, fmt.Errorf("Unexpected tag %s, line %d\n",
					               string(restag.val), p.scanner.line)
				}
			}

			parent = append(parent, res)
		}
		t = nil
		goto restart
	}
	return parent, err
}

func (p *Parser) parse(t *tag, parent interface{}) (ret interface{}, err error) {
	defer func() {
		p.err = err
	}()

restart:
	if t == nil {
		t, err = p.nexttag()
		if err != nil {
			return nil, err
		}
	}

	switch t.state {
	case TAG, QUOTE, VQUOTE, SLASH:
		// new key
		k := string(t.val)

		themap, ok := parent.(map[string] interface{})
		if !ok {
			debug("not a map at tag:", k)
			panic("...")
		}

		korder_intf, ok := themap[KeyOrder]
		var korder []string
		if !ok {
			if UclExportKeyOrder {
				// only initialize if requested
				korder = make([]string, 0, 16)
			}
		} else {
			korder, ok = korder_intf.([]string)
			if !ok {
				debug("key order is not slice")
				return nil, fmt.Errorf("map[--keyorder--] is not slice")
			}
		}

		res, err := p.parsevalue(nil, nil)
		if err != nil {
			if restag, ok := res.(*tag); ok {
				if restag.state == SEMICOL {
					// no value for key, make it == null
					res = nil
				}
			} else {
				debug("parsevalue error:", err)
				return nil, err
			}
		} else if restag, ok := res.(*tag); ok {
			// result is a tag; parsevalue didn't handle it
			if restag.state != BRACECLOSE {
				t = restag
				goto restart
			}
			res = string(restag.val)
			t = restag
		}

		if mapitems, ok := themap[k]; ok {
			if childarray, ok := mapitems.([]interface{}); ok {
				// already an array, so append
				childarray = append(childarray, res)
				themap[k] = childarray
			} else {
				childarray := make([]interface{}, 1, 2)
				childarray[0] = themap[k]
				childarray = append(childarray, res)
				themap[k] = childarray
			}
		} else {
			// doesn't exist
			if cap(korder) != 0 {
				// only update KeyOrder if it was initialized
				korder = append(korder, k)
				themap[KeyOrder] = korder
			}
			themap[k] = res
		}
		if t.state == BRACECLOSE {
			// map completed
			return parent, nil
		}

		// done for this tag; go to next
		t = nil
		goto restart

	case SEMICOL:
		t = nil
		goto restart

	case MLSTRING:
		// shouldn't happen
		return nil, fmt.Errorf("Unexpected multi-line string")

	case BRACEOPEN:
		// {
		// If parent is not a map and not nil, then error

		var theparent interface{}
		var ok bool
		if parent == nil {
			theparent = make(map[string] interface{})
		} else if theparent, ok = parent.(map[string] interface{}); !ok {
			if theparent, ok = parent.([]interface{}); !ok {
				debug("Error braceopen - parent is not a map/list/nil")
				return nil, fmt.Errorf("Invalid {, parent not nil|map|list")
			}
		}
		res, err := p.parse(nil, theparent)
		if err != nil {
			debug("Error parsing brace", err)
		}
		return res, err

	case BRACECLOSE:
		// map finished
		return parent, nil

	case BRACKETOPEN:
		thelist := make([]interface{}, 0, 32)
		res, err := p.parselist(nil, thelist)
		return res, err

	case BRACKETCLOSE:
		// list finished
		return parent, nil
	}

	return nil, nil
}


func (p *Parser) Ucl() (map[string] interface{}, error) {
	p.parse(nil, p.ucl)

	if p.err == io.EOF {
		p.err = nil
	}
	return p.ucl, p.err
}
