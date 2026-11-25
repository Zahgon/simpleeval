// Ported from CPython 3.12 (Lib/warnings.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Warnings: what the parser and the values issue, and how a caller records
// them.

package py

import (
	"fmt"
	"os"
	"sync"
)

// Warning is one issued warning: the name of its category class and its
// message.
type Warning struct {
	Category string
	Message  string
}

var (
	warnMu       sync.Mutex
	warnRecorder []*[]Warning
)

// Warn issues a warning of the category. Inside CatchWarnings it is
// recorded; otherwise it is written to standard error.
func Warn(category, message string) {
	warnMu.Lock()
	defer warnMu.Unlock()
	if n := len(warnRecorder); n > 0 {
		rec := warnRecorder[n-1]
		*rec = append(*rec, Warning{Category: category, Message: message})
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", category, message)
}

// CatchWarnings runs fn and returns every warning issued while it ran, in
// order, as warnings.catch_warnings(record=True) records them under the
// "always" filter.
func CatchWarnings(fn func()) []Warning {
	rec := new([]Warning)
	warnMu.Lock()
	warnRecorder = append(warnRecorder, rec)
	warnMu.Unlock()
	defer func() {
		warnMu.Lock()
		warnRecorder = warnRecorder[:len(warnRecorder)-1]
		warnMu.Unlock()
	}()
	fn()
	return *rec
}
