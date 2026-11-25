// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// What libraries add to the builtin values: classes defined outside the
// builtins module (and the instances of them a Go program describes), and
// modules.

package py

import (
	"sync"

	"github.com/danthedeckie/simpleeval/internal/textutil"
)

var (
	libraryMu      sync.Mutex
	libraryClasses = map[string]*classV{}
)

// DefineClass defines the class module.name deriving from the builtin or
// library class named base (object when base is empty), and returns it.
// Defining the same name again returns the class defined first.
func DefineClass(module, name, base string) Value { return defineClass(module, name, base) }

func defineClass(module, name, base string) *classV {
	libraryMu.Lock()
	defer libraryMu.Unlock()
	if k, ok := libraryClasses[name]; ok {
		return k
	}
	parent := objectClass
	if k, ok := libraryClasses[base]; ok {
		parent = k
	} else if k, ok := classes[base]; ok {
		parent = k
	}
	k := &classV{name: name, module: module, base: parent}
	libraryClasses[name] = k
	return k
}

// Object is an instance of a library class, described by the Go program
// that makes it: Module and Class name its class, Attrs holds its
// attributes, and each hook left nil means the class does not define that
// special method. GetAttr is __getattr__: it is called for a name Attrs
// does not hold. Repr falls back on object.__repr__ and Str on Repr, as
// object does.
type Object struct {
	Module  string
	Class   string
	Attrs   map[string]Value
	GetAttr func(name string) (Value, error)
	Repr    func() (string, error)
	Str     func() (string, error)
	Call    func(args []Value, kwargs []Keyword) (Value, error)
	GetItem func(key Value) (Value, error)
	Len     func() (int, error)
}

// NewObject returns the instance o describes; its class is defined with
// DefineClass(o.Module, o.Class, "") unless it already is.
func NewObject(o Object) Value {
	c := o
	c.Attrs = make(map[string]Value, len(o.Attrs))
	for k, v := range o.Attrs {
		c.Attrs[k] = v
	}
	defineClass(o.Module, o.Class, "")
	return &c
}

func (o *Object) pyType() string { return o.Class }

func (o *Object) class() *classV { return defineClass(o.Module, o.Class, "") }

func (o *Object) repr(*reprState) (string, error) {
	if o.Repr != nil {
		s, err := o.Repr()
		return s, fromGoError(err)
	}
	return "<" + o.Module + "." + o.Class + " object at " + objAddr(o) + ">", nil
}

func (o *Object) str() (string, error) {
	if o.Str != nil {
		s, err := o.Str()
		return s, fromGoError(err)
	}
	return o.repr(nil)
}

func (o *Object) getAttr(name string) (Value, error) {
	if v, ok := o.Attrs[name]; ok {
		return v, nil
	}
	if o.GetAttr != nil && name != "__class__" {
		v, err := o.GetAttr(name)
		return v, fromGoError(err)
	}
	return nil, nil
}

func (o *Object) isCallable() bool { return o.Call != nil }

func (o *Object) pyCall(args []Value, kw *dictV) (Value, error) {
	if o.Call == nil {
		return nil, typeErr("'%s' object is not callable", o.Class)
	}
	return wrapGoFunc(o.Call)(args, kw)
}

func (o *Object) getItem(key Value) (Value, error) {
	if o.GetItem == nil {
		return nil, typeErr("'%s' object is not subscriptable", o.Class)
	}
	v, err := o.GetItem(key)
	return v, fromGoError(err)
}

// truth is bool(o): its __len__ when the class defines one, else True.
func (o *Object) truth() (bool, error) {
	if o.Len == nil {
		return true, nil
	}
	n, err := o.Len()
	return n > 0, fromGoError(err)
}

func (o *Object) length() (int, error) {
	if o.Len == nil {
		return 0, typeErr("object of type '%s' has no len()", o.Class)
	}
	n, err := o.Len()
	return n, fromGoError(err)
}

// moduleV is a module object.
type moduleV struct {
	name  string
	attrs *dictV
}

func (*moduleV) pyType() string { return "module" }

func (m *moduleV) repr(*reprState) (string, error) {
	return "<module " + textutil.Repr(m.name) + " (built-in)>", nil
}

func (m *moduleV) getAttr(name string) (Value, error) {
	if v, ok := m.attrs.getStr(name); ok {
		return v, nil
	}
	if name == "__name__" {
		return strV(m.name), nil
	}
	return nil, nil
}

func (m *moduleV) getAttrErr(name string) error {
	return newPyErr("AttributeError", "module '"+m.name+"' has no attribute '"+name+"'")
}

var (
	moduleMu      sync.Mutex
	moduleMakers  = map[string]func() []moduleAttr{}
	moduleObjects = map[string]*moduleV{}
)

// moduleAttr is one attribute of a module, in the order dir() lists them.
type moduleAttr struct {
	name  string
	value Value
}

// registerModule makes a standard-library module available by name: attrs
// builds its attributes, once, on first use.
func registerModule(name string, attrs func() []moduleAttr) {
	moduleMu.Lock()
	defer moduleMu.Unlock()
	moduleMakers[name] = attrs
}

// Module is the standard-library module of the given name ("math",
// "operator", "os" or "posixpath", which is os.path), or nil when there is
// none.
func Module(name string) Value {
	if m := module(name); m != nil {
		return m
	}
	return nil
}

func module(name string) *moduleV {
	moduleMu.Lock()
	m, done := moduleObjects[name]
	mk, known := moduleMakers[name]
	moduleMu.Unlock()
	if done {
		return m
	}
	if !known {
		return nil
	}
	// The attributes are built without the lock held: a module may hold
	// another (os holds os.path).
	m = &moduleV{name: name, attrs: newDict()}
	for _, a := range mk() {
		m.attrs.setStr(a.name, a.value)
	}
	moduleMu.Lock()
	defer moduleMu.Unlock()
	if first, ok := moduleObjects[name]; ok {
		return first
	}
	moduleObjects[name] = m
	return m
}
