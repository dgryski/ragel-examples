
This post shows how to use ragel to speed up regular expression matching in Go.

The Go `regexp` package is a simplified port of the `re2` regular expression
engine from Google (https://github.com/google/re2).  It provides linear-time
matching for regular expressions.  It is able to provide this guarantee because
it only permits regexps that are Regular Expressions in the formal computer
science sense of the term. This is in contrast to libraries like PCRE which
provide a richer input language (and thus are able to match more complex
things) but at the cost of potentially pathological exponential run times.

For many cases, PCRE is in fact faster than `regexp`.  PCRE has had many years
of optimization work; Go's regexp package is being kept deliberately simple.
(Russ Cox, the author of both `regexp` and `re2` has expressed his desire to
prevent `regexp` from becoming as over-engineered as `re2`.)  In addition,
while Go's code generation has been improving with every release (notably with
the new backend in 1.7), C code is still faster on average than equivalent Go
code.

So, if you want to use regular expressions in Go, should you switch to PCRE?
Probably not.  Go's regular expression package is fast enough for most uses. If
you're writing a server, guaranteed linear time matching from `regexp`
eliminates a class of security and performance issues.  Having a crash due to
exponential-time matching on pathological inputs for some regular expressions
present in backtracking engines is a real concern.  StackOverflow was brought
down for precisely this reason:
http://stackstatus.net/post/147710624694/outage-postmortem-july-20-2016 . The
`re2` C++ library was written for Google Code Search to prevent this class of
problems too.  Finally, you don't have to fight with `cgo` in order to use it.

How can you speed up regular expression matching in Go? As Rob Pike points out,
the benefit of regular expressions is their dynamic nature.  If you know what
you're going to be matching, you can do better.  Sometimes `doing better` just
means a for loop with an if check. (
https://commandcenter.blogspot.ca/2011/08/regular-expressions-in-lexing-and.html
)

Occasionally, though, you really do have complex regular expressions you need to
match against. Perhaps you're porting code from a different language where
regexps are heavily used.  Perhaps you're doing text processing and a for-loop
just won't cut it.

If Go's regular expression engine turns out to be the bottleneck, you can use
ragel to generate the state machine to match your regexp ahead of time.  Rather
than being interpreted through the regexp engine, ragel produces Go code that
can be compiled into your package.  This can easily give 7x-10x speedups.

Lets walk through a simple example:

To build this, first we need ragel.  Many distributions package ragel already,
so installing it could be as easy as:
```
apt-get install ragel OR yum install ragel
```

or grab the source from the home page https://www.colm.net/open-source/ragel/ :

```
curl -O https://www.colm.net/files/ragel/ragel-6.9.tar.gz
tar xf ragel-6.9.tar.gz 
cd ragel-6.9
./configure && make && make install
```

Next, check that ragel is in your path:

```
bash$ ragel -version
Ragel State Machine Compiler version 6.9 Oct 2014
Copyright (c) 2001-2009 by Adrian Thurston
```

We'll start with an example from the post that inspired me to write this
tutorial: http://crypticjags.com/golang/can-golang-beat-perl-on-regex-performance.html

The blog post was comparing the speed of Perl to Go's native regexp implementation
with the following expression:

`sshd\[\d{5}\]:\s*Failed`

We can benchmark this with the following framework in `sshd_test.go`:

```go
package main

import (
	"regexp"
	"testing"
)

// sample input line from the blog post
var data = []byte(`Jan 18 06:41:30 corecompute sshd[42327]: Failed keyboard-interactive/pam for root from 112.100.68.182 port 48803 ssh2`)

// make sure the benchmark isn't optimized away
var hits int

var reSSHD = regexp.MustCompile(`sshd\[\d{5}\]:\s*Failed`)

func BenchmarkRegex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if reSSHD.Match(data) {
			hits++
		}
	}
}
```

To turn our regex into something we can use with ragel, we can use the
following template (in `sshd.rl`):

```go
package main

func matchSSHD(data []byte) bool {

%% machine scanner;
%% write data;

	cs, p, pe, eof := 0, 0, len(data), len(data)

        _ = eof

	%%{
	    main := any* 'sshd[' digit{5} ']:' space* 'Failed' @{ return true } ;

	    write init;
	    write exec;
	}%%

        return false
}
```

There's more boilerplate for matchers created with ragel, but we can still see
the regexp there.  The `@{ return true }` says that if the state machine gets
to that point, the action should be to return `true` from the function.
Otherwise, execution falls through the state machine and we return `false` for
no match.  The input language is fully documented in the manual available
online at https://www.colm.net/files/ragel/ragel-guide-6.9.pdf

Next, tell ragel to compile this into a Go state machine:
```sh
bash$ ragel -Z sshd.rl 
```

This produces `sshd.go` containing table-driven generated code.  (We will see
other matcher machine types later.)

Add the following benchmark to `sshd_test.go`
```go
func BenchmarkRagel(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if matchSSHD(data) {
			hits++
		}
	}
}
```

We can now benchmark the difference between them:
```
bash$ go version
go version go1.7rc3 linux/amd64
bash$ go test -test.bench=.
testing: warning: no tests to run
BenchmarkRegex-4   	 3000000	       441 ns/op
BenchmarkRagel-4   	 5000000	       360 ns/op
PASS
ok  	github.com/dgryski/ragel-examples/regexp1	3.944s
```

The default machine type for ragel is a table-based matcher, which is not much
faster than Go's regular engine and in some cases might be slower.  Go
basically computes similar tables at regexp compile time, so evaluation is
similar.  There are other goto-based machines that can be used instead by
passing `-G0`, `-G1`, or `-G2`.  These matchers are increasingly faster at the
cost of larger binaries. (For this simple example, the differences in binary
sizes are small). We will see other cases later where we can use more features
from ragel to make matching more powerful.

We can see the difference in speed between the matcher types with the following
command line:

```sh
bash$ for opt in "" -G0 -G1 -G2; do echo "opt=$opt"; ragel $opt -Z sshd.rl; go test -test.bench=Ragel; done 
# slightly edited output
opt=
BenchmarkRagel-4   	 3000000	       368 ns/op
opt=-G0
BenchmarkRagel-4   	10000000	       142 ns/op
opt=-G1
BenchmarkRagel-4   	10000000	       136 ns/op
opt=-G2
BenchmarkRagel-4   	20000000	        65.9 ns/op
```

Running these a bunch more and tweaking the output so we can compare the
benchmarks directly with Russ Cox's benchstat utility (
https://godoc.org/rsc.io/benchstat ), we can see the matcher built
with `-G2` is a significant win over the regular matcher.

```
bash$ benchstat bench.old bench.new
name     old time/op  new time/op  delta
Regex-4   448ns ± 3%    66ns ± 3%  -85.36%  (p=0.000 n=17+19)
```
