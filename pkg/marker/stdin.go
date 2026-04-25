package marker

import "fmt"

// delimiterPrefix is the fixed prefix for every per-invocation heredoc
// delimiter. The suffix is a unique 8-hex-char ID generated at call time.
const delimiterPrefix = "A2S_STDIN_"

// GenerateDelimiter returns a fresh heredoc delimiter of the form
// "A2S_STDIN_<8hex>". The suffix is derived from GenerateID(), ensuring
// practical uniqueness across invocations.
func GenerateDelimiter() string {
	return delimiterPrefix + GenerateID()
}

// WrapStdinCommand wraps cmd and stdinContent into a subshell heredoc pipeline:
//
//	(cat <<'A2S_STDIN_<8hex>' | cmd
//	<stdinContent>
//	A2S_STDIN_<8hex>
//	)
//
// The subshell parentheses make the heredoc a single compound command that
// works with WrapCommand's semicolon-separated format:
//
//	echo 'START'; (cat <<'DELIM' | cmd
//	<content>
//	DELIM
//	); echo 'END'$?
//
// Without the subshell, the shell would see "; echo 'END'" on a line after
// the heredoc delimiter, causing a syntax error from the leading semicolon.
//
// The heredoc is single-quoted (cat <<'DELIM') so the shell treats the body
// as literal text — no variable expansion or backslash interpretation.
//
// When stdinContent is empty, cmd is returned unchanged.
func WrapStdinCommand(cmd, stdinContent string) string {
	if stdinContent == "" {
		return cmd
	}
	delim := GenerateDelimiter()
	// Ensure stdinContent ends with a newline so the delimiter is on its own
	// line. Most scripts already end with \n; this handles edge cases.
	body := stdinContent
	if len(body) == 0 || body[len(body)-1] != '\n' {
		body += "\n"
	}
	return fmt.Sprintf("(cat <<'%s' | %s\n%s%s\n)", delim, cmd, body, delim)
}
