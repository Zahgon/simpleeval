"""Write unicode_names.bin: the character names of the running CPython, as
the \\N{...} escape and the namereplace error handler use them.

The names come from unicodedata.name. Names of the form PREFIX-XXXX, whose
hex part is the code point (ideographs and the like), are kept as ranges;
the Hangul syllables and the CJK unified ideographs are left to the code,
which builds their names from the code point. The aliases, which unicodedata
cannot list, are the candidates of Perl's copy of the Unicode name list
(unicore/Name.pl) that this interpreter's \\N{...} accepts. Every name is
checked against the interpreter itself. Run with the pinned CPython 3.12
from this directory:

    devtools/py.sh gold/internal/textutil python3 gen_names.py

File format (integers are unsigned LEB128 varints, strings a varint length
then the bytes):

    words:   count, then each word
    names:   count, then for each: code point delta from the previous entry,
             a flag (1 when \\N{...} accepts the name), the number of words,
             and the index of each word (the words are joined by spaces)
    ranges:  count, then for each: prefix, first and last code point, flag
    aliases: count, then for each: code point and name
    unified: count, then the first and last code point of each range of the
             CJK unified ideographs
"""

import re
import unicodedata

HANGUL = range(0xAC00, 0xAC00 + 11172)
HEX_NAME = re.compile(r'^(.*-)([0-9A-F]{4,6})$')


def is_unified_ideograph(c):
    return unicodedata.name(chr(c), '').startswith('CJK UNIFIED IDEOGRAPH-')


def escape_accepts(name):
    try:
        return eval('"\\N{%s}"' % name)
    except SyntaxError:
        return None


def uvarint(n):
    out = bytearray()
    while True:
        b = n & 0x7f
        n >>= 7
        if n:
            out.append(b | 0x80)
        else:
            out.append(b)
            return bytes(out)


def string(s):
    b = s.encode('ascii')
    return uvarint(len(b)) + b


def main():
    names, ranges = [], []
    for c in range(0x110000):
        name = unicodedata.name(chr(c), None)
        if name is None or c in HANGUL or is_unified_ideograph(c):
            continue
        # namereplace writes the name the escape reads back.
        assert c < 0x80 or chr(c).encode('ascii', 'namereplace') == ('\\N{%s}' % name).encode(), hex(c)
        accepted = escape_accepts(name) == chr(c)
        m = HEX_NAME.match(name)
        if m and int(m.group(2), 16) == c:
            prefix = m.group(1)
            if ranges and ranges[-1][0] == prefix and ranges[-1][2] == c - 1 and ranges[-1][3] == accepted:
                ranges[-1][2] = c
            else:
                ranges.append([prefix, c, c, accepted])
            continue
        names.append((c, accepted, name))
    for c in list(HANGUL) + [c for c in range(0x110000) if is_unified_ideograph(c)]:
        assert escape_accepts(unicodedata.name(chr(c))) == chr(c), hex(c)
    official = {n for _, _, n in names}
    aliases = {}
    with open('/usr/share/perl/5.36.0/unicore/Name.pl', encoding='ascii') as f:
        for line in f:
            cand = line.strip()
            if not re.fullmatch(r'[A-Z][A-Z0-9 -]*', cand) or cand in official:
                continue
            r = escape_accepts(cand)
            if r is not None and len(r) == 1 and unicodedata.name(r, None) != cand:
                aliases[cand] = ord(r)
    words = sorted({w for _, _, n in names for w in n.split(' ')})
    index = {w: i for i, w in enumerate(words)}
    out = bytearray(uvarint(len(words)))
    for w in words:
        out += string(w)
    out += uvarint(len(names))
    prev = 0
    for c, accepted, name in names:
        ws = name.split(' ')
        out += uvarint(c - prev) + uvarint(int(accepted)) + uvarint(len(ws))
        for w in ws:
            out += uvarint(index[w])
        prev = c
    out += uvarint(len(ranges))
    for prefix, lo, hi, accepted in ranges:
        out += string(prefix) + uvarint(lo) + uvarint(hi) + uvarint(int(accepted))
    out += uvarint(len(aliases))
    for name in sorted(aliases):
        out += uvarint(aliases[name]) + string(name)
    unified = []
    for c in range(0x110000):
        if is_unified_ideograph(c):
            if unified and unified[-1][1] == c - 1:
                unified[-1][1] = c
            else:
                unified.append([c, c])
    out += uvarint(len(unified))
    for lo, hi in unified:
        out += uvarint(lo) + uvarint(hi)
    with open('unicode_names.bin', 'wb') as f:
        f.write(out)
    print(len(words), len(names), len(ranges), len(aliases), len(out))


if __name__ == '__main__':
    main()
