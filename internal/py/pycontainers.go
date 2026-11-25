// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// dict, set and frozenset. A dict keeps insertion order; a set keeps CPython 3.12's open-addressing
// table (setobject.c), so that it iterates, prints and pops its elements in CPython's order.

package py

// ---------------------------------------------------------------- dict

// dictV is an insertion-ordered dict. hkeys holds the hashKey of each key,
// which index maps to the key's position.
type dictV struct {
	keys  []Value
	vals  []Value
	hkeys []any
	index map[any]int
}

func newDict() *dictV { return &dictV{index: map[any]int{}} }

func (*dictV) pyType() string { return "dict" }

func (d *dictV) set(key, value Value) error {
	k, err := hashKey(key)
	if err != nil {
		return err
	}
	d.setHashed(k, key, value)
	return nil
}

// setStr is set for a str key, whose hash cannot fail.
func (d *dictV) setStr(key string, value Value) { d.setHashed(strKey(key), strV(key), value) }

func (d *dictV) setHashed(k any, key, value Value) {
	if i, ok := d.index[k]; ok {
		d.vals[i] = value
		return
	}
	d.index[k] = len(d.keys)
	d.keys = append(d.keys, key)
	d.vals = append(d.vals, value)
	d.hkeys = append(d.hkeys, k)
}

func (d *dictV) get(key Value) (Value, bool, error) {
	k, err := hashKey(key)
	if err != nil {
		return nil, false, err
	}
	if i, ok := d.index[k]; ok {
		return d.vals[i], true, nil
	}
	return nil, false, nil
}

func (d *dictV) getStr(key string) (Value, bool) {
	if i, ok := d.index[strKey(key)]; ok {
		return d.vals[i], true
	}
	return nil, false
}

// pop removes key and returns its value, if the dict has it.
func (d *dictV) pop(key Value) (Value, bool, error) {
	k, err := hashKey(key)
	if err != nil {
		return nil, false, err
	}
	i, ok := d.index[k]
	if !ok {
		return nil, false, nil
	}
	v := d.vals[i]
	d.removeAt(i)
	return v, true, nil
}

func (d *dictV) del(key Value) (bool, error) {
	_, found, err := d.pop(key)
	return found, err
}

// removeAt removes the item at position i, keeping the order of the others.
func (d *dictV) removeAt(i int) {
	delete(d.index, d.hkeys[i])
	d.keys = append(d.keys[:i:i], d.keys[i+1:]...)
	d.vals = append(d.vals[:i:i], d.vals[i+1:]...)
	d.hkeys = append(d.hkeys[:i:i], d.hkeys[i+1:]...)
	for j := i; j < len(d.hkeys); j++ {
		d.index[d.hkeys[j]] = j
	}
}

func (d *dictV) clear() { d.keys, d.vals, d.hkeys, d.index = nil, nil, nil, map[any]int{} }

func (d *dictV) copy() *dictV {
	n := &dictV{keys: append([]Value(nil), d.keys...), vals: append([]Value(nil), d.vals...),
		hkeys: append([]any(nil), d.hkeys...), index: make(map[any]int, len(d.keys))}
	for k, v := range d.index {
		n.index[k] = v
	}
	return n
}

// ---------------------------------------------------------------- set

const (
	setMinSize      = 8
	setLinearProbes = 9
	setPerturbShift = 5
)

type setEntry struct {
	key   Value // nil for an unused slot
	hash  int64
	dummy bool // a deleted slot
}

func (e setEntry) live() bool { return e.key != nil && !e.dummy }

// setV is a set, or a frozenset when frozen is true.
type setV struct {
	table  []setEntry
	fill   int // live and dummy slots
	used   int // live slots
	finger int
	frozen bool
}

func newSet(frozen bool) *setV {
	return &setV{table: make([]setEntry, setMinSize), frozen: frozen}
}

func (s *setV) pyType() string {
	if s.frozen {
		return "frozenset"
	}
	return "set"
}

func (s *setV) mask() int { return len(s.table) - 1 }

// items returns the elements in table order.
func (s *setV) items() []Value {
	out := make([]Value, 0, s.used)
	for _, e := range s.table {
		if e.live() {
			out = append(out, e.key)
		}
	}
	return out
}

func (s *setV) length() (int, error) { return s.used, nil }

func (s *setV) iter() (iterator, error) {
	size, i := s.used, 0
	return funcIter(func() (Value, bool, error) {
		if s.used != size {
			size = -1
			return nil, false, newPyErr("RuntimeError", "Set changed size during iteration")
		}
		for i < len(s.table) {
			e := s.table[i]
			i++
			if e.live() {
				return e.key, true, nil
			}
		}
		return nil, false, nil
	}), nil
}

// find returns the slot of key, or -1.
func (s *setV) find(key Value, hash int64) (int, error) {
	mask := s.mask()
	perturb := uint64(hash)
	i := int(uint64(hash) & uint64(mask))
	for {
		probes := 0
		if i+setLinearProbes <= mask {
			probes = setLinearProbes
		}
		j := i
		for {
			e := s.table[j]
			if e.key == nil && !e.dummy {
				return -1, nil
			}
			if e.live() && e.hash == hash {
				if sameObject(e.key, key) {
					return j, nil
				}
				eq, err := pyEq(e.key, key)
				if err != nil {
					return -1, err
				}
				if eq {
					return j, nil
				}
			}
			if probes == 0 {
				break
			}
			probes--
			j++
		}
		perturb >>= setPerturbShift
		i = int((uint64(i)*5 + 1 + perturb) & uint64(mask))
	}
}

func (s *setV) addHashed(key Value, hash int64) error {
	mask := s.mask()
	perturb := uint64(hash)
	i := int(uint64(hash) & uint64(mask))
	free := -1 // the first dummy slot passed, which an insertion reuses
	for {
		probes := 0
		if i+setLinearProbes <= mask {
			probes = setLinearProbes
		}
		j := i
		for {
			e := &s.table[j]
			if e.key == nil && !e.dummy {
				if free >= 0 {
					s.used++
					s.table[free] = setEntry{key: key, hash: hash}
					return nil
				}
				s.fill++
				s.used++
				e.key, e.hash = key, hash
				if s.fill*5 < mask*3 {
					return nil
				}
				if s.used > 50000 {
					s.resize(s.used * 2)
				} else {
					s.resize(s.used * 4)
				}
				return nil
			}
			if e.live() && e.hash == hash {
				if sameObject(e.key, key) {
					return nil
				}
				eq, err := pyEq(e.key, key)
				if err != nil {
					return err
				}
				if eq {
					return nil
				}
			} else if e.dummy && free < 0 {
				free = j
			}
			if probes == 0 {
				break
			}
			probes--
			j++
		}
		perturb >>= setPerturbShift
		i = int((uint64(i)*5 + 1 + perturb) & uint64(mask))
	}
}

func (s *setV) add(key Value) error {
	h, err := pyHash(key)
	if err != nil {
		return err
	}
	return s.addHashed(key, h)
}

func insertClean(table []setEntry, key Value, hash int64) {
	mask := len(table) - 1
	perturb := uint64(hash)
	i := int(uint64(hash) & uint64(mask))
	for {
		if table[i].key == nil {
			table[i] = setEntry{key: key, hash: hash}
			return
		}
		if i+setLinearProbes <= mask {
			for j := 1; j <= setLinearProbes; j++ {
				if table[i+j].key == nil {
					table[i+j] = setEntry{key: key, hash: hash}
					return
				}
			}
		}
		perturb >>= setPerturbShift
		i = int((uint64(i)*5 + 1 + perturb) & uint64(mask))
	}
}

func (s *setV) resize(minused int) {
	size := setMinSize
	for size <= minused {
		size <<= 1
	}
	old := s.table
	s.table = make([]setEntry, size)
	for _, e := range old {
		if e.live() {
			insertClean(s.table, e.key, e.hash)
		}
	}
	s.fill = s.used
}

// discard removes key and reports whether it was present.
func (s *setV) discard(key Value) (bool, error) {
	h, err := pyHash(key)
	if err != nil {
		return false, err
	}
	j, err := s.find(key, h)
	if err != nil || j < 0 {
		return false, err
	}
	s.table[j] = setEntry{hash: -1, dummy: true}
	s.used--
	return true, nil
}

func (s *setV) contains(key Value) (bool, error) {
	h, err := pyHash(key)
	if err != nil {
		if sk, ok := key.(*setV); ok && !sk.frozen {
			// set.__contains__ retries an unhashable set as a frozenset.
			fk := setCopy(sk, true)
			j, err := s.find(fk, frozensetHash(fk))
			return j >= 0, err
		}
		return false, err
	}
	j, err := s.find(key, h)
	return j >= 0, err
}

func (s *setV) clear() {
	s.table = make([]setEntry, setMinSize)
	s.fill, s.used, s.finger = 0, 0, 0
}

// fillFrom is set_merge into the empty set s, which never compares
// elements and so cannot fail.
func (s *setV) fillFrom(other *setV) {
	if other.used == 0 {
		return
	}
	if other.used*5 >= s.mask()*3 {
		s.resize(other.used * 2)
	}
	if s.mask() == other.mask() && other.fill == other.used {
		copy(s.table, other.table)
		s.fill, s.used = other.fill, other.used
		return
	}
	for _, e := range other.table {
		if e.live() {
			insertClean(s.table, e.key, e.hash)
		}
	}
	s.fill, s.used = other.used, other.used
}

// merge is set_merge: other's elements added to s.
func (s *setV) merge(other *setV) error {
	if other == s || other.used == 0 {
		return nil
	}
	if s.fill == 0 {
		s.fillFrom(other)
		return nil
	}
	if (s.fill+other.used)*5 >= s.mask()*3 {
		s.resize((s.used + other.used) * 2)
	}
	for _, e := range other.table {
		if e.live() {
			if err := s.addHashed(e.key, e.hash); err != nil {
				return err
			}
		}
	}
	return nil
}

// update is set_update_internal: the elements of any iterable added to s.
func (s *setV) update(v Value) error {
	switch o := v.(type) {
	case *setV:
		return s.merge(o)
	case *dictV:
		if (s.fill+len(o.keys))*5 >= s.mask()*3 {
			s.resize((s.used + len(o.keys)) * 2)
		}
		for _, k := range o.keys {
			if err := s.add(k); err != nil {
				return err
			}
		}
		return nil
	}
	it, err := pyIter(v)
	if err != nil {
		return err
	}
	for {
		item, ok, err := it.next()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := s.add(item); err != nil {
			return err
		}
	}
}

// pop is set.pop(): the first live slot from the finger on.
func (s *setV) pop() (Value, error) {
	if s.used == 0 {
		return nil, newExc("KeyError", []Value{strV("pop from an empty set")})
	}
	i := s.finger & s.mask()
	for !s.table[i].live() {
		i++
		if i > s.mask() {
			i = 0
		}
	}
	key := s.table[i].key
	s.table[i] = setEntry{hash: -1, dummy: true}
	s.used--
	s.finger = i + 1
	return key, nil
}

func toSet(v Value) (*setV, error) {
	if s, ok := v.(*setV); ok {
		return s, nil
	}
	s := newSet(true)
	if err := s.update(v); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *setV) subsetOf(o *setV) (bool, error) {
	if s.used > o.used {
		return false, nil
	}
	for _, e := range s.table {
		if e.live() {
			j, err := o.find(e.key, e.hash)
			if err != nil || j < 0 {
				return false, err
			}
		}
	}
	return true, nil
}

func (s *setV) eq(other Value) (bool, error) {
	o, ok := other.(*setV)
	if !ok {
		return false, nil
	}
	if s.used != o.used {
		return false, nil
	}
	return s.subsetOf(o)
}

// setCompare is set_richcompare for the ordering operators: subset and
// superset tests.
func setCompare(op string, a, b *setV) (bool, error) {
	switch op {
	case "<":
		if a.used >= b.used {
			return false, nil
		}
		return a.subsetOf(b)
	case "<=":
		return a.subsetOf(b)
	case ">":
		if b.used >= a.used {
			return false, nil
		}
		return b.subsetOf(a)
	}
	return b.subsetOf(a)
}

func (s *setV) repr(st *reprState) (string, error) { return s.reprAs(s.pyType(), !s.frozen, st) }

// reprAs is the repr of the set as an instance of the class named name: the
// braces alone for a non-empty set when bare (an instance of set itself),
// name(...) otherwise.
func (s *setV) reprAs(name string, bare bool, st *reprState) (string, error) {
	items := s.items()
	if len(items) == 0 {
		return name + "()", nil
	}
	if !st.enter(s) {
		return name + "(...)", nil
	}
	defer st.leave()
	body, err := seqRepr("{", "}", items, false, st)
	if err != nil || bare {
		return body, err
	}
	return name + "(" + body + ")", nil
}
