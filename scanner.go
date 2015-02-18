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
	"bytes"
	"io"
	"strings"
	"strconv"
	"unicode/utf8"
	"errors"
)

const (
	WHITESPACE = iota
	TAG
	SEMICOL          // semi-colon ;
	COMMA            // ,
	COLON            // :
	EQUAL            // =
	QUOTE            // double quote string
	VQUOTE           // single quote string
	SLASH            // regex or /* */ comment
	HCOMMENT         // # ...
	LCOMMENT         // /* ... */
	MLSTRING         // <<EOD multi-line string

	BRACEOPEN
	BRACECLOSE

	BRACKETOPEN
	BRACKETCLOSE

	// scanner indicators only:

	LCOMMENT_CLOSING // Not a tag, just indicator of possibly '*/'
	                 // closing indicator
	MAYBE_MLSTRING
	MAYBE_MLSTRING2
	MLSTRING_PREP
	MLSTRING_HEADER_OK
)

const (
	skip_white = 0x01
	skip_sep   = 0x02
)

type tag struct {
	val   []byte
	state int

	flag  int       // used by parser
}

var UnexpectedEOF = errors.New("Unexpected EOF")

type scanner struct {
	r      io.Reader
	buf    []byte
	bufmax int
	bufi   int

	depth  []byte    // current depth of scopes, e.g. [ '[', '{' ]
	                 // to determine when the scope closes
	curtag []byte
	curch  byte

	state  int
	skipsep int

	line   int       // current input line

	mlstring_tag []byte // "EOD" tag of ML string
	curline []byte

	err    error
}

func newScanner(rio io.Reader) *scanner {
	return &scanner{
	                 r: rio,
	                 depth: make([]byte, 0, 1024),
	                 curtag: make([]byte, 0, 1024),
	                 line: 1,
	               }
}

func (s *scanner) scopeadd(c byte) {
	s.depth = append(s.depth, c)
}

func (s *scanner) scopereduce(c byte) bool {
	if len(s.depth) == 0 {
		return false
	}

	found := false
	switch s.depth[len(s.depth)-1] {
	case '[':
		if c == ']' {
			s.depth = s.depth[:len(s.depth)-1]
			found = true
		}

	case '{':
		if c == '}' {
			s.depth = s.depth[:len(s.depth)-1]
			found = true
		}

	case '(':
		if c == ')' {
			s.depth = s.depth[:len(s.depth)-1]
			found = true
		}
	}
	return found
}

func (s *scanner) curdepth() byte {
	if len(s.depth) == 0 {
		return 0
	}
	return s.depth[len(s.depth)-1]
}

func (s *scanner) discard() {
	s.curtag = make([]byte, 0, 1024)
}

func (s *scanner) maketag(v []byte, state int) (t *tag) {
	t = new(tag)
	if v != nil {
		if len(v) > 0 {
			t.val = make([]byte, len(v))
			copy(t.val, v)
			t.state = state
		}
	} else if s.state == QUOTE || s.state == VQUOTE {
		t.state = s.state
		var c byte
		if s.state == QUOTE {
			c = '"'
		} else {
			c = '\''
		}
		qs, err := unquote(string(s.curtag), c)
		if err != nil {
			s.err = fmt.Errorf("unable to unquote \"%s\", line %d",
			                   string(s.curtag), s.line)
			return nil
		}
		t.val = []byte(qs)
		s.curtag = s.curtag[:0]
	} else if len(s.curtag) > 0 {
		t.state = s.state
		t.val = s.curtag
		s.curtag = make([]byte, 0, 1024)
	}
	return t
}

func (s *scanner) nexttags() (tags []*tag, err error) {
	err = nil

	if s.buf == nil {
		s.buf = make([]byte, 4096)
	}

	tags = make([]*tag, 0, 32)
	for {
		if s.bufi >= s.bufmax {
			s.bufmax, err = s.r.Read(s.buf)
			if s.bufmax == 0 {
				if len(s.depth) > 0 {
					return nil, UnexpectedEOF
				} else {
					return nil, io.EOF
				}
			}

			if err != nil {
				return nil, err
			}

			s.bufi = 0
		}

		c := s.buf[s.bufi]
		s.bufi++

		if c == '\n' {
			s.line++
		}

		switch s.state {
		case WHITESPACE, BRACEOPEN, BRACECLOSE:

			if c <= ' ' {
				if s.state != WHITESPACE {
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
				}
				s.curtag = append(s.curtag, c)
				s.state = WHITESPACE
				if len(tags) > 0 {
					return tags, nil
				}
				break
			}

			// emit now that end of whitespace reached
			if len(s.curtag) > 0 && s.curtag[len(s.curtag)-1] <= ' ' {
				s.discard()
				/*
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				*/
			}

			if c != '"' && c != '\'' {
				s.curtag = append(s.curtag, c)
			}
			switch c {
			case '[', ']':
				if c == '[' {
					s.scopeadd(c)
					s.state = BRACKETOPEN
				} else {
					if !s.scopereduce(c) {
						return nil, fmt.Errorf("misplaced ] at line %d", s.line)
					}
					s.state = BRACKETCLOSE
				}

				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil

			case '(':
				s.state = TAG
				s.skipsep = skip_white | skip_sep

			case ')':
				s.state = TAG
				s.skipsep = skip_white | skip_sep

			case '{', '}':
				if c == '{' {
					s.scopeadd(c)
					s.state = BRACEOPEN
				} else {
					if !s.scopereduce(c) {
						return nil, fmt.Errorf("misplaced } at line %d", s.line)
					}
					s.state = BRACECLOSE
				}
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil

			case '/':
				s.state = SLASH

			case '"':
				s.state = QUOTE

			case '\'':
				s.state = VQUOTE

			case '#':
				s.state = HCOMMENT

			case '<':
				s.state = TAG
				s.skipsep = skip_white | skip_sep

			case '=', ':':
				if len(tags) == 0 ||
				   (tags[len(tags)-1].state != QUOTE &&
				    tags[len(tags)-1].state != VQUOTE) {
					return nil, fmt.Errorf("unexpected '%c' at line %d", c, s.line)
				}
				s.state = TAG
				s.skipsep = skip_white

			case ',':
				if s.curdepth() == '[' {
					s.state = COMMA
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
					s.state = WHITESPACE
					return tags, nil
				} else {
					return nil, fmt.Errorf("unexpected ',' at line %d", s.line)
				}

			case ';':
				s.state = SEMICOL
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil

			default:
				// all other characters commence a new tag
				s.state = TAG
				s.skipsep = skip_white | skip_sep
			}

		case TAG:
			// read until either ; { or '\n'
			// if {, then split tag into different keys and send each tag
			// as a TAG
			if len(s.curtag) > 0 {
				if s.curtag[len(s.curtag)-1] == '<' {
					// possibly multiline string if next character
					// is alphanum
					s.curtag = append(s.curtag, c)
					s.state = MAYBE_MLSTRING
					break
				}
			}

			if c == '{' {
				// split up tag into individual strings, separated by ' '
				fields := strings.Split(string(s.curtag), " ")
				for f := range fields {
					if fields[f] != "" {
						tags = append(tags, s.maketag([]byte(fields[f]), TAG))
						if s.err != nil {
							return nil, s.err
						}
					}
				}
				s.curtag = s.curtag[:0]
				s.curtag = append(s.curtag, c)
				s.scopeadd(c)
				s.state = BRACEOPEN
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil

			} else if c == '}' {
				if s.curdepth() != '{' {
					return nil, fmt.Errorf("unexpected } at line %d", s.line)
				}

				// scan backwards and terminate previous tag
				for i := len(s.curtag)-1; i >= 0; i-- {
					if s.curtag[i] > ' ' {
						tags = append(tags, s.maketag(s.curtag[0:i+1], TAG))
						if s.err != nil {
							return nil, s.err
						}
						break
					}
				}
				if !s.scopereduce(c) {
					panic("shouldn't happen")
				}

				tags = append(tags, s.maketag([]byte("}"), BRACECLOSE))
				if s.err != nil {
					return nil, s.err
				}
				s.curtag = s.curtag[:0]
				s.state = WHITESPACE
				return tags, nil

			} else if c == '\'' || c == '"' {
				for i := len(s.curtag)-1; i >= 0; i-- {
					if s.curtag[i] > ' ' {
						tags = append(tags, s.maketag(s.curtag[0:i+1], TAG))
						if s.err != nil {
							return nil, s.err
						}
						break
					}
				}
				s.curtag = s.curtag[:0]
				if c == '\'' {
					s.state = VQUOTE
				} else {
					s.state = QUOTE
				}
				return tags, nil

			} else if c == '[' {
				// split up tag into individual strings, separated by ' '
				fields := strings.Split(string(s.curtag), " ")
				for f := range fields {
					if fields[f] != "" {
						tags = append(tags, s.maketag([]byte(fields[f]), TAG))
						if s.err != nil {
							return nil, s.err
						}
					}
				}
				s.curtag = s.curtag[:0]
				s.curtag = append(s.curtag, c)
				s.scopeadd(c)
				s.state = BRACKETOPEN
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil

			} else if c == ']' {
				if s.curdepth() != '[' {
					return nil, fmt.Errorf("unexpected } at line %d", s.line)
				}

				// scan backwards and terminate previous tag
				for i := len(s.curtag)-1; i >= 0; i-- {
					if s.curtag[i] > ' ' {
						tags = append(tags, s.maketag(s.curtag[0:i+1], TAG))
						if s.err != nil {
							return nil, s.err
						}
						break
					}
				}
				if !s.scopereduce(c) {
					panic("shouldn't happen")
				}
				tags = append(tags, s.maketag([]byte("]"), BRACKETCLOSE))
				if s.err != nil {
					return nil, s.err
				}
				s.curtag = s.curtag[:0]
				s.state = WHITESPACE
				return tags, nil

			} else if c == ';' {
				// Terminate
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.curtag = s.curtag[:0]
				s.curtag = append(s.curtag, c)
				s.state = SEMICOL
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil

			} else if c == ',' {
				if s.curdepth() != '[' && s.curdepth() != '{' {
					s.curtag = append(s.curtag, c)
					break
				}
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.curtag = s.curtag[:0]
				s.curtag = append(s.curtag, c)
				s.state = COMMA
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil

			} else if c == '\n' {
				// TODO: option for semicolon forced termination
				//s.curtag = append(s.curtag, ' ')
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				tags = append(tags, s.maketag([]byte(";"), SEMICOL))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				s.curtag = s.curtag[:0]
				return tags, nil

			} else if c <= ' ' {
				if len(tags) == 0 {
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
				} else if s.skipsep & skip_white == 0 {
					s.curtag = append(s.curtag, c)
				}

			} else if c == ':' || c == '=' {
				if s.skipsep & skip_sep != 0 {
					// only skip the first seen : or =, after that
					// it is considered a part of the tag's value
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
					s.curtag = s.curtag[:0]
					s.curtag = append(s.curtag, c)
					if c == ':' {
						s.state = COLON
					} else {
						s.state = EQUAL
					}
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
					s.state = TAG
					s.skipsep &= ^skip_sep
				} else {
					s.curtag = append(s.curtag, c)
					s.skipsep &= ^skip_white
				}

			} else if c == '\\' {
				return nil, fmt.Errorf("unexpected '%c' at line %d", c,
					s.line)

			} else {
				s.curtag = append(s.curtag, c)
				if len(tags) > 0 {
					s.skipsep &= ^skip_white
				}
			}

		case MAYBE_MLSTRING:
			s.curtag = append(s.curtag, c)
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			   c >= '0' && c <= '9' {
				s.state = MLSTRING_PREP
				s.curline = make([]byte, 0, 128)
				s.curline = append(s.curline, c)
			} else {
				s.state = TAG
			}

		case MLSTRING_PREP:
			// still on <<EOD multiline header line
			// continue until EOL
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			   c >= '0' && c <= '9' {
				// XXX Send the tag key
				if s.curtag[0] != '<' {
					ti := 0
					for ; ti < len(s.curtag); ti++ {
						if s.curtag[ti] <= ' ' {
							break
						}
					}
					te := ti
					for ; te < len(s.curtag); te++ {
						if s.curtag[te] == '<' {
							break
						}
						if s.curtag[te] > ' ' && s.curtag[te] != '=' {
							return nil, fmt.Errorf("Line %d %c %d %s", s.line, s.curtag[te], te, string(s.curtag))
						}
					}
					tags = append(tags, s.maketag(s.curtag[:ti], TAG))
					if s.err != nil {
						return nil, s.err
					}

					s.curtag = s.curtag[te:]
				}

				s.curtag = append(s.curtag, c)
				s.curline = append(s.curline, c)

				if len(tags) > 0 {
					return tags, nil
				}

			} else {
				// end of "EOD" tag
				s.mlstring_tag = make([]byte, len(s.curline))
				copy(s.mlstring_tag, s.curline)
				s.curline = nil
				s.curtag = s.curtag[:0]
				if c == '\n' {
					s.state = MLSTRING
				} else {
					// discard junk after "EOD"
					s.state = MLSTRING_HEADER_OK
				}
			}

		case MLSTRING_HEADER_OK:
			// read and skip to eol
			if c == '\n' {
				s.state = MLSTRING
			}

		case MLSTRING:
			// read until we see "EOD" on its own line
			if s.curline == nil {
				s.curline = make([]byte, 0, 128)
			}
			if c == ';' || c == '\n' {
				if bytes.Equal(s.curline, s.mlstring_tag) {
					// "EOD" reached
					s.curtag = s.curtag[:len(s.curtag)-1]
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
					s.state = WHITESPACE
				} else {
					s.curtag = append(s.curtag, s.curline...)
					s.curtag = append(s.curtag, c)
				}
				s.curline = nil
			} else {
				s.curline = append(s.curline, c)
			}
			if len(tags) > 0 {
				return tags, nil
			}

		case QUOTE, VQUOTE:
			// read until quote completes, allow for multi-line
			if c == '\\' {
				if s.bufi >= s.bufmax {
					s.curtag = append(s.curtag, c)
					break
				}
				s.curtag = append(s.curtag, c)
				c = s.buf[s.bufi]
				s.curtag = append(s.curtag, c)
				if c == '\n' {
					s.line++
				}
				s.bufi++
				break

			} else if (s.state == QUOTE && c == '"') ||
			          (s.state == VQUOTE && c == '\'') {
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = TAG
				s.skipsep = skip_white | skip_sep
			} else {
				s.curtag = append(s.curtag, c)
			}

		case SLASH:
			// read until next slash or whitespace. regex with whitespace
			// have an outer quote

			if len(s.curtag) == 1 && c == '*' {
				s.curtag = append(s.curtag, c)
				s.state = LCOMMENT
			} else {
				if c == '\\' {
					// Escape sequence
					if s.bufi+1 < s.bufmax {
						s.curtag = append(s.curtag, c)
						s.bufi++
						s.curtag = append(s.curtag, s.buf[s.bufi])
						s.bufi++
						break
					}
				}

				switch c {
				case '/':
					s.curtag = append(s.curtag, c)
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
					s.state = WHITESPACE
					return tags, nil

				case '\n':
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
					s.state = WHITESPACE
					return tags, nil

				case ' ':
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
					s.curtag = append(s.curtag, c)
					s.state = WHITESPACE
					return tags, nil

				case ';':
					s.state = TAG
					tags = append(tags, s.maketag(nil, 0))
					if s.err != nil {
						return nil, s.err
					}
					s.curtag = append(s.curtag, c)
					s.state = TAG
					return tags, nil

				default:
					s.curtag = append(s.curtag, c)
				}
			}

		case HCOMMENT:
			// single line comment
			if c == '\n' {
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil
			} else {
				s.curtag = append(s.curtag, c)
			}

		case LCOMMENT:
			s.curtag = append(s.curtag, c)
			if c == '*' {
				s.state = LCOMMENT_CLOSING
			}

		case LCOMMENT_CLOSING:
			s.curtag = append(s.curtag, c)
			if c == '/' {
				s.state = LCOMMENT
				tags = append(tags, s.maketag(nil, 0))
				if s.err != nil {
					return nil, s.err
				}
				s.state = WHITESPACE
				return tags, nil
			} else {
				s.state = LCOMMENT
			}
		}
	}

	return nil, err
}

func (s *scanner) LatestTag() (string, int) {
	return string(s.curtag), s.state
}


// From go-src:strconv.Unquote but modified so that a quote character can
// be provided instead of requiring the string to be pre-quoted
func unquote(s string, quote byte) (t string, err error) {
	n := len(s)
	if n == 0 {
		return "", nil
	}

	if quote == '`' {
		if contains(s, '`') {
			return "", strconv.ErrSyntax
		}
		return s, nil
	}
	if quote != '"' && quote != '\'' {
		return "", strconv.ErrSyntax
	}

	// Is it trivial?  Avoid allocation.
	if !contains(s, '\\') && !contains(s, quote) {
		return s, nil
	}

	var runeTmp [utf8.UTFMax]byte
	buf := make([]byte, 0, 3*len(s)/2) // Try to avoid more allocations.
	for len(s) > 0 {
		c, multibyte, ss, err := strconv.UnquoteChar(s, quote)
		if err != nil {
			return "", err
		}
		s = ss
		if c < utf8.RuneSelf || !multibyte {
			buf = append(buf, byte(c))
		} else {
			n := utf8.EncodeRune(runeTmp[:], c)
			buf = append(buf, runeTmp[:n]...)
		}
	}
	return string(buf), nil
}

func contains(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}
