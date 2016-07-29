package main

import (
	"regexp"
	"testing"
)

var data = []byte(`Jan 18 06:41:30 corecompute sshd[42327]: Failed keyboard-interactive/pam for root from 112.100.68.182 port 48803 ssh2`)

var hits int

var reSSHD = regexp.MustCompile(`sshd\[\d{5}\]:\s*Failed`)

func BenchmarkRegex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if reSSHD.Match(data) {
			hits++
		}
	}
}

func BenchmarkRagel(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if matchSSHD(data) {
			hits++
		}
	}
}
