// Ported from CPython 3.12 (Lib/codecs.py, Lib/encodings, Objects/unicodeobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

// Package codecs implements Python's text codecs as CPython 3.12 does: the
// encoding names and aliases codecs.lookup accepts, bytes.decode and str.encode
// with the strict, ignore, replace, backslashreplace, surrogateescape,
// surrogatepass and (for encoding) xmlcharrefreplace and namereplace error
// handlers, and the incremental reading of
// codecs.getreader(encoding)(stream).read(size).
//
// The codecs are utf-8, utf-8-sig, ascii, latin-1, utf-16, utf-16-le,
// utf-16-be, utf-32, utf-32-le, utf-32-be and every single-byte charmap codec
// of the encodings package (cp1252, iso8859-15, koi8-r, mac-roman, ...); for
// any other name Lookup, Decode and Encode raise LookupError "unknown
// encoding". ModuleOf is the name search over all the modules of CPython's
// encodings package, which pyexpat's unknown encoding handler needs. As in
// CPython, str.encode and bytes.decode refuse a codec that is not a text
// encoding (base64, zlib, rot13, ...).
//
// Python str values are Go strings holding UTF-8, a lone surrogate in the
// generalized form package textutil reads and writes; positions in str values
// (UnicodeEncodeError.start and end, StreamReader sizes) count code points.
// Failures are Python exceptions (see package pyexc); those of the codecs
// themselves are *Error, which adds the UnicodeError attributes.
package codecs
