// Package scrub removes secrets from shell commands before they are stored.
//
// Scrubbing runs in layers: private-key blocks, known token formats,
// structural rules (KEY=value, --password flags, URL credentials, auth
// headers), and finally a high-entropy fallback. Each secret is replaced with
// <REDACTED:kind> so the command stays readable.
package scrub

import (
	"regexp"
	"strings"
)

// Command is a shell command that has been through Scrub. Its fields are
// unexported so the only way to obtain one is by scrubbing, which lets
// storage code require scrubbed input at compile time.
type Command struct {
	text       string
	redactions int
}

// Text returns the scrubbed command text.
func (c Command) Text() string { return c.text }

// Redactions returns how many secrets were replaced.
func (c Command) Redactions() int { return c.redactions }

// Ignored reports whether raw must not be recorded at all. Following the
// shell ignorespace convention, a command starting with a space is private.
func Ignored(raw string) bool {
	return strings.HasPrefix(raw, " ")
}

// Scrub returns raw with surrounding whitespace trimmed and every detected
// secret replaced by <REDACTED:kind>.
func Scrub(raw string) Command {
	text := strings.TrimSpace(raw)
	total := 0
	for _, r := range rules {
		var n int
		text, n = r.applyAll(text)
		total += n
	}
	text, n := entropyRule.applyAll(text)
	return Command{text: text, redactions: total + n}
}

const redactedPrefix = "<REDACTED:"

type rule struct {
	kind  string
	re    *regexp.Regexp
	group int // submatch to replace; 0 replaces the whole match
	// skip, if set, vetoes redaction of the match described by m.
	skip func(s string, m []int) bool
}

// applyAll applies r until it finds nothing new. One pass is not enough for
// rules anchored on a program name: in "mysql -pA -pB" the match for -pA
// consumes "mysql", so -pB only matches on the next pass. Every pass redacts
// at least one value and redacted values never match again, so this ends.
func (r rule) applyAll(s string) (string, int) {
	total := 0
	for {
		var n int
		s, n = r.apply(s)
		if n == 0 {
			return s, total
		}
		total += n
	}
}

func (r rule) apply(s string) (string, int) {
	matches := r.re.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s, 0
	}
	var b strings.Builder
	last, n := 0, 0
	for _, m := range matches {
		start, end := m[2*r.group], m[2*r.group+1]
		if start < 0 || !isSecretValue(s[start:end]) || (r.skip != nil && r.skip(s, m)) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(redactedPrefix)
		b.WriteString(r.kind)
		b.WriteByte('>')
		last = end
		n++
	}
	if n == 0 {
		return s, 0
	}
	b.WriteString(s[last:])
	return b.String(), n
}

// isSecretValue reports whether a matched value could be a secret. Shell
// variable references and command substitutions ($TOKEN, ${TOKEN},
// $(pass show x)) only name a secret, and already-redacted values are done.
func isSecretValue(v string) bool {
	v = strings.Trim(v, `"'`)
	return v != "" && !strings.HasPrefix(v, "$") && !strings.Contains(v, redactedPrefix)
}

// value matches a shell word: double-quoted, single-quoted, or unquoted up to
// whitespace or a shell metacharacter.
const value = `("[^"]*"|'[^']*'|[^\s'"` + "`" + `;&|<>()]+)`

func token(kind, pattern string) rule {
	return rule{kind: kind, re: regexp.MustCompile(pattern)}
}

func field(kind, pattern string) rule {
	return rule{kind: kind, re: regexp.MustCompile(pattern), group: 1}
}

// rules run in order. Earlier, more specific rules win: once a value is
// redacted, later rules skip it.
var rules = []rule{
	// Layer 1: private key blocks, possibly spanning lines.
	token("private-key", `(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY[A-Z ]*-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY[A-Z ]*-----|\z)`),

	// Layer 2: known token formats.
	token("aws-access-key", `\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`),
	token("github-token", `\bgh[pousr]_[A-Za-z0-9]{36,255}\b`),
	token("github-token", `\bgithub_pat_[A-Za-z0-9_]{22,255}\b`),
	token("gitlab-token", `\bglpat-[A-Za-z0-9_-]{20,}`),
	token("slack-token", `\bxox[abposr]-[A-Za-z0-9-]{10,}`),
	token("slack-webhook", `https://hooks\.slack\.com/services/[A-Za-z0-9_/+-]+`),
	token("stripe-key", `\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}\b`),
	token("google-api-key", `\bAIza[0-9A-Za-z_-]{35}`),
	token("anthropic-key", `\bsk-ant-[A-Za-z0-9_-]{20,}`),
	token("openai-key", `\bsk-(?:proj|svcacct|admin)-[A-Za-z0-9_-]{20,}`),
	token("openai-key", `\bsk-[A-Za-z0-9]{48}\b`),
	token("huggingface-token", `\bhf_[A-Za-z0-9]{30,}\b`),
	token("npm-token", `\bnpm_[A-Za-z0-9]{36}\b`),
	token("pypi-token", `\bpypi-[A-Za-z0-9_-]{50,}`),
	token("digitalocean-token", `\bdo[por]_v1_[a-f0-9]{64}\b`),
	token("sendgrid-key", `\bSG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}\b`),
	token("jwt", `\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`),

	// Layer 3: secrets identified by where they appear.
	// User and password classes exclude < and > so an earlier <REDACTED:kind>
	// (which contains a colon) is never mistaken for user:password.
	field("url-password", `(?i)\b[a-z][a-z0-9+.-]*://[^/\s:@'"<>]+:([^@\s/'"<>]+)@`),
	field("auth-header", `(?i)\b(?:proxy-)?authorization\s*:\s*(?:(?:bearer|basic|token|digest|negotiate)\s+)?([^\s'"]+)`),
	field("api-key-header", `(?i)\b(?:x-api-key|api-key|x-auth-token|x-access-token|private-token|x-amz-security-token|x-vault-token)\s*:\s*([^\s'"]+)`),
	field("cookie", `(?i)\bcookie\s*:\s*([^'"\n]+)`),
	field("json-secret", `(?i)"[a-z0-9_]*(?:password|passwd|secret|token|api_?key|access_?key)"\s*:\s*("[^"]*")`),
	field("query-secret", `(?i)[?&](?:key|api_?key|token|access_token|auth|sig|signature|password|secret|client_secret|x-amz-signature|x-amz-credential|x-amz-security-token)=([^&\s'"#]+)`),
	field("password", `\b(?:mysql|mysqldump|mysqladmin|mysqlsh|mariadb|mariadb-dump)\b[^\n|;&]*?\s-p`+value),
	field("password", `\bsshpass\s+-p\s*`+value),
	field("password", `\bredis-cli\b[^\n|;&]*?\s-a\s+`+value),
	field("password", `\b(?:curl|http|https|xh)\b[^\n|;&]*?\s(?:-u|--user)[=\s]*['"]?[^:\s'"<>]+:([^\s'"<>]+)`),
	field("password", `\bpass:`+value),
	field("aws-secret", `(?i)\baws\s+configure\s+set\s+(?:[a-z0-9_-]+\.)?(?:aws_secret_access_key|aws_session_token)\s+`+value),
	{
		kind:  "secret-flag",
		re:    regexp.MustCompile(`(?i)(?:^|\s)--?(?:[a-z0-9]+[-_])*(?:password|passwd|pass|token|secret|api[-_]?key|access[-_]?key|secret[-_]?key|client[-_]?secret|auth[-_]?token|credentials?)(?:=|\s+)` + value),
		group: 1,
		skip:  func(s string, m []int) bool { return strings.HasPrefix(s[m[2]:m[3]], "-") },
	},
	{
		kind:  "secret-env",
		re:    regexp.MustCompile(`(?i)\b((?:[a-z0-9]+_)*(?:password|passwd|passphrase|pass|pwd|secret|token|apikey|credentials?|(?:api|access|secret|private|master|encryption|signing|client)_key)(?:_[a-z0-9]+)*)=` + value),
		group: 2,
		skip:  func(s string, m []int) bool { return nonSecretKey(s[m[2]:m[3]]) },
	},
}

// nonSecretKey reports whether an assignment key that mentions a secret
// actually holds something else, like a path to a token file.
func nonSecretKey(key string) bool {
	key = strings.ToUpper(key)
	if key == "PWD" {
		return true
	}
	last := key[strings.LastIndexByte(key, '_')+1:]
	switch last {
	case "FILE", "PATH", "DIR", "ID", "NAME", "TYPE", "URL", "HOST", "PORT",
		"USER", "USERNAME", "LENGTH", "TTL", "EXPIRY", "ALGORITHM":
		return true
	}
	return false
}
