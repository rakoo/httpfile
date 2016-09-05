/*
Copyright 2011 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

func TestSum(t *testing.T) {
	var buf [100000]uint8
	rnd := rand.New(rand.NewSource(4))
	for i := range buf {
		buf[i] = uint8(rnd.Intn(256))
	}

	sum := func(offset, len int) uint32 {
		rs := NewRollSum()
		for count := offset; count < len; count++ {
			rs.Roll(buf[count])
		}
		return rs.Digest()
	}

	sum1a := sum(0, len(buf))
	sum1b := sum(1, len(buf))
	sum2a := sum(len(buf)-windowSize*5/2, len(buf)-windowSize)
	sum2b := sum(0, len(buf)-windowSize)
	sum3a := sum(0, windowSize+3)
	sum3b := sum(3, windowSize+3)

	if sum1a != sum1b {
		t.Errorf("sum1a=%d sum1b=%d", sum1a, sum1b)
	}
	if sum2a != sum2b {
		t.Errorf("sum2a=%d sum2b=%d", sum2a, sum2b)
	}
	if sum3a != sum3b {
		t.Errorf("sum3a=%d sum3b=%d", sum3a, sum3b)
	}
}

type res struct {
	chunk  []byte
	digest uint32
	bits   int
	err    error
}

func (r res) Equal(o res) bool {
	return bytes.Compare(r.chunk, o.chunk) == 0 &&
		r.digest == o.digest &&
		r.bits == o.bits &&
		r.err == o.err
}

func (r res) String() string {
	return fmt.Sprintf("[chunk: %d, digest: %d, bits: %d, err: %v]",
		len(r.chunk), r.digest, r.bits, r.err)
}

func BenchmarkRollsum(b *testing.B) {
	const bufSize = 5 << 20
	buf := make([]byte, bufSize)
	for i := range buf {
		buf[i] = byte(rand.Int63())
	}

	b.ResetTimer()
	rs := NewRollSum()
	splits := 0
	for i := 0; i < b.N; i++ {
		splits = 0
		for _, b := range buf {
			rs.Roll(b)
			if rs.OnSplit() {
				_ = rs.Bits()
				splits++
			}
		}
	}
	b.SetBytes(bufSize)
	b.Logf("num splits = %d; every %d bytes", splits, int(float64(bufSize)/float64(splits)))
}
