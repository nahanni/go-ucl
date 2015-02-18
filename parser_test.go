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
	"testing"
	"encoding/json"
	"bytes"
	"time"
	"os"
	"io"
)

func BenchmarkParser(b *testing.B) {
	s := `
section {
    foo = bar;
    abc zyx;
    foo = z;
	t {
        child_of a t;
    }
	foo {
		another one {
			two 3;
		}
		another three {
			is 5;
		}
	}
      # a hash comment

      x /some_regex/;

      zz "ABC
CDE";

      mlstring = <<EODX
This is something
of

a 
long
string  .
.
EODX
}
`
	b.StopTimer()
	for n := 0; n < b.N; n++ {
		bb := bytes.NewBuffer([]byte(s))
		p := NewParser(bb)
		b.StartTimer()
		ucl, uerr := p.Ucl()
		b.StopTimer()
		if uerr == nil || uerr == io.EOF {
			if _, ok := ucl["section"]; !ok {
				b.Fatal("Ucl parse failed", ucl)
			}
		}
	}
	b.Log("Total loops:", b.N)
}



func TestParser (t *testing.T) {
	s := `
section {
    foo = bar;

    abc zyx;
	quoted "quoted";
	internalquoted "internally\"quoted with 'single quote'";
	quotedmulti "quote
edmulti";
    foo = z;
	t {
        child_of a t;
    }
	foo {
		another one {
			two 3;
			three 4;
		}
		another three {
			is 5;
		}

		multi field "value";
	}

    list [
		{ a: 123 } ];
    multilist [ "1",
	"2",
	3
	];

      # a hash comment
      /* A comment that's */

      x /some_regex/;

      zz "ABC CDE";

      mlstring = <<EODX
This is something
of

a 
long
string  .
.
EODX;
	none; # this is a null value
	emptystr "";
	single-quote 'Single"Quote';
	"Quoted\"key" 'som\'evalue';
	mustquote "adsfasf:asdfsa";
}
`
	var err error
	bb := bytes.NewBuffer([]byte(s))
	p := NewParser(bb)
	tstart := time.Now().UnixNano()
	ucl, uerr := p.Ucl()
	tend := time.Now().UnixNano()
	tdiff := tend - tstart
	t.Log("Total time: ", tdiff, "ns", "--->err:", uerr)

	var b []byte
	b, err = json.MarshalIndent(ucl, "", "   ")
	if err != nil {
		t.Log("Error marshling ucl", err, uerr)
	} else {
		t.Log("RESULT:\n", string(b), uerr)
	}

	os.Stdout.Write([]byte("ENCODE >>\n"))
	Encode(os.Stdout, ucl, "\t", "json", "")


	// Byte-level accuracy test
	var ibuf bytes.Buffer
	Encode(&ibuf, ucl, "   ", "json", "")

	b1 := ibuf.Bytes()

	p = NewParser(&ibuf)
	ucl, uerr = p.Ucl()

	var obuf bytes.Buffer
	Encode(&obuf, ucl, "   ", "json", "")

	b2 := obuf.Bytes()
	if len(b1) != len(b2) {
		t.Log("Byte lengths differ", len(b1), "vs", len(b2))
		t.Log("b1:", string(b1))
		t.Log("b2:", string(b2))
		t.Error("Encoding accuracy test failed")
	}

	for i := range b1 {
		if b1[i] != b2[i] {
			t.Errorf("byte at %d differ [%x] [%x]\n", i, b1[i], b2[i])
		}
	}
	t.Log("***** OK! *****")
	t.Log("\n"+string(b1))



	t.Log("Testing anonymous and struct encoding")

	type anon struct {
		Anon  string `json:"anon"`
		Anon2 string `json:"anon2"`
	}
	type nilanon struct {
		A string `json:"a"`
	}
	var ss struct {
		*anon
		*nilanon
		A int `json:"a"`
		B string
		C struct {
			D int `json:"d"`
		} `json:"c"`
		NilVal *int `json:"nilval"`
	}
	ss.A = 3
	ss.B = "Something"
	ss.C.D = 10
	ss.anon = new(anon)
	ss.anon.Anon = "anon value"
	ibuf.Reset()
	Encode(&ibuf, &ss, "   ", "json", `""`)
	t.Log("\n" + ibuf.String())
}
