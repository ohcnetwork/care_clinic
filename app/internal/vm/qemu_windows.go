package vm

import "strings"

// qemuEscape doubles commas, QEMU's escape inside option values.
func qemuEscape(s string) string { return strings.ReplaceAll(s, ",", ",,") }
