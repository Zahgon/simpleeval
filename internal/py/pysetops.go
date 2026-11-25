// Ported from CPython 3.12 (Objects/setobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The set algebra of set and frozenset, as setobject.c computes it: which
// operand is iterated, which elements are inserted into which table and
// when tables are copied or resized decide the order the result iterates and
// prints in.

package py

// setCopy is set_copy: a new set of the given kind with s's elements merged
// into an empty table.
func setCopy(s *setV, frozen bool) *setV {
	r := newSet(frozen)
	r.fillFrom(s)
	return r
}

// setUnionMulti is set.union(*others): a copy of s updated with each other
// iterable in turn.
func setUnionMulti(s *setV, others []Value) (*setV, error) {
	r := setCopy(s, s.frozen)
	for _, o := range others {
		if o == Value(s) {
			continue
		}
		if err := r.update(o); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// setIntersection is set_intersection: the elements of the smaller operand
// (or of a non-set iterable, in its order) found in the other, inserted into
// a new set of s's kind.
func setIntersection(s *setV, other Value) (*setV, error) {
	if other == Value(s) {
		return setCopy(s, s.frozen), nil
	}
	r := newSet(s.frozen)
	if o, ok := other.(*setV); ok {
		big, small := s, o
		if o.used > s.used {
			big, small = o, s
		}
		for _, e := range small.table {
			if !e.live() {
				continue
			}
			j, err := big.find(e.key, e.hash)
			if err != nil {
				return nil, err
			}
			if j >= 0 {
				if err := r.addHashed(e.key, e.hash); err != nil {
					return nil, err
				}
			}
		}
		return r, nil
	}
	it, err := pyIter(other)
	if err != nil {
		return nil, err
	}
	for {
		item, more, err := it.next()
		if err != nil || !more {
			return r, err
		}
		h, err := pyHash(item)
		if err != nil {
			return nil, err
		}
		j, err := s.find(item, h)
		if err != nil {
			return nil, err
		}
		if j >= 0 {
			if err := r.addHashed(item, h); err != nil {
				return nil, err
			}
			if r.used >= s.used {
				return r, nil
			}
		}
	}
}

// setIntersectionMulti is set.intersection(*others).
func setIntersectionMulti(s *setV, others []Value) (*setV, error) {
	if len(others) == 0 {
		return setCopy(s, s.frozen), nil
	}
	r := s
	for _, o := range others {
		var err error
		if r, err = setIntersection(r, o); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// setDifference is set_difference: for a set or dict other that is not much
// smaller than s, the elements of s not in other inserted into a new set;
// otherwise a copy of s with other's elements discarded.
func setDifference(s *setV, other Value) (*setV, error) {
	var otherSize int
	switch o := other.(type) {
	case *setV:
		otherSize = o.used
	case *dictV:
		otherSize = len(o.keys)
	default:
		return setCopyAndDifference(s, other)
	}
	if s.used>>2 > otherSize {
		return setCopyAndDifference(s, other)
	}
	r := newSet(s.frozen)
	for _, e := range s.table {
		if !e.live() {
			continue
		}
		var in bool
		var err error
		if o, ok := other.(*setV); ok {
			j, ferr := o.find(e.key, e.hash)
			in, err = j >= 0, ferr
		} else {
			_, in, err = other.(*dictV).get(e.key)
		}
		if err != nil {
			return nil, err
		}
		if !in {
			if err := r.addHashed(e.key, e.hash); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

func setCopyAndDifference(s *setV, other Value) (*setV, error) {
	r := setCopy(s, s.frozen)
	if err := r.differenceUpdate(other); err != nil {
		return nil, err
	}
	return r, nil
}

// setDifferenceMulti is set.difference(*others).
func setDifferenceMulti(s *setV, others []Value) (*setV, error) {
	if len(others) == 0 {
		return setCopy(s, s.frozen), nil
	}
	r, err := setDifference(s, others[0])
	if err != nil {
		return nil, err
	}
	for _, o := range others[1:] {
		if err := r.differenceUpdate(o); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// differenceUpdate is set_difference_update_internal: other's elements
// discarded from s, then the table rebuilt when more than a quarter of it
// is dummies.
func (s *setV) differenceUpdate(other Value) error {
	if other == Value(s) {
		s.clear()
		return nil
	}
	if o, ok := other.(*setV); ok {
		if o.used>>3 > s.used {
			var err error
			if o, err = setIntersection(s, o); err != nil {
				return err
			}
		}
		for _, e := range o.table {
			if !e.live() {
				continue
			}
			j, err := s.find(e.key, e.hash)
			if err != nil {
				return err
			}
			if j >= 0 {
				s.table[j] = setEntry{hash: -1, dummy: true}
				s.used--
			}
		}
	} else {
		it, err := pyIter(other)
		if err != nil {
			return err
		}
		for {
			item, more, err := it.next()
			if err != nil {
				return err
			}
			if !more {
				break
			}
			if _, err := s.discard(item); err != nil {
				return err
			}
		}
	}
	if s.fill-s.used <= s.mask()/4 {
		return nil
	}
	if s.used > 50000 {
		s.resize(s.used * 2)
	} else {
		s.resize(s.used * 4)
	}
	return nil
}

// setSymmetricDifference is set_symmetric_difference: a set of s's kind
// built from other, with each element of s toggled in it.
func setSymmetricDifference(s *setV, other Value) (*setV, error) {
	r := newSet(s.frozen)
	if err := r.update(other); err != nil {
		return nil, err
	}
	if err := r.symmetricDifferenceUpdate(s); err != nil {
		return nil, err
	}
	return r, nil
}

// symmetricDifferenceUpdate is set_symmetric_difference_update: each
// element of other removed from s when present and added when not.
func (s *setV) symmetricDifferenceUpdate(other Value) error {
	if other == Value(s) {
		s.clear()
		return nil
	}
	toggle := func(key Value, hash int64) error {
		j, err := s.find(key, hash)
		if err != nil {
			return err
		}
		if j >= 0 {
			s.table[j] = setEntry{hash: -1, dummy: true}
			s.used--
			return nil
		}
		return s.addHashed(key, hash)
	}
	if d, ok := other.(*dictV); ok {
		for _, k := range d.keys {
			h, err := pyHash(k)
			if err != nil {
				return err
			}
			if err := toggle(k, h); err != nil {
				return err
			}
		}
		return nil
	}
	o, ok := other.(*setV)
	if !ok {
		o = newSet(s.frozen)
		if err := o.update(other); err != nil {
			return err
		}
	}
	for _, e := range o.table {
		if e.live() {
			if err := toggle(e.key, e.hash); err != nil {
				return err
			}
		}
	}
	return nil
}

// isDisjoint is set.isdisjoint(other).
func (s *setV) isDisjoint(other Value) (bool, error) {
	if other == Value(s) {
		return s.used == 0, nil
	}
	if o, ok := other.(*setV); ok {
		big, small := s, o
		if o.used > s.used {
			big, small = o, s
		}
		for _, e := range small.table {
			if !e.live() {
				continue
			}
			j, err := big.find(e.key, e.hash)
			if err != nil || j >= 0 {
				return false, err
			}
		}
		return true, nil
	}
	it, err := pyIter(other)
	if err != nil {
		return false, err
	}
	for {
		item, more, err := it.next()
		if err != nil || !more {
			return true, err
		}
		in, err := s.contains(item)
		if err != nil || in {
			return false, err
		}
	}
}

// isSuperset is set.issuperset(other): for a set, other.issubset(s);
// otherwise every item of the iterable is in s.
func (s *setV) isSuperset(other Value) (bool, error) {
	if o, ok := other.(*setV); ok {
		return o.subsetOf(s)
	}
	it, err := pyIter(other)
	if err != nil {
		return false, err
	}
	for {
		item, more, err := it.next()
		if err != nil || !more {
			return true, err
		}
		in, err := s.contains(item)
		if err != nil || !in {
			return false, err
		}
	}
}
