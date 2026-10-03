package proc

import "regexp"

// NetworkError retains the command's original failure without exposing its output.
type NetworkError struct {
	Err error
}

func (e *NetworkError) Error() string { return e.Err.Error() }
func (e *NetworkError) Unwrap() error { return e.Err }

// Old, recovered download warnings must not classify a later build failure.
const networkDiagnosticTail = 20

var networkFailure = regexp.MustCompile(`(?i)(could(?:n't| not) resolve (?:host|proxy)|temporary failure in name resolution|no such host|network is (?:unreachable|down)|connection reset by peer|tls handshake timeout|i/o timeout|\b(?:ECONNRESET|ETIMEDOUT|ENOTFOUND|EAI_AGAIN|ENETUNREACH)\b|(?:https?://|download|fetch).*(?:unexpected EOF|connection reset|connection refused|timed out|timeout|empty reply))`)
