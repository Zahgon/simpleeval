"""Write unicode_tables.go: the character data of the running CPython's str.

Every character class and case mapping the module needs (str.isspace,
str.isalpha, ..., str.lower, str.upper, str.title, str.casefold, the Cased
and Case_Ignorable properties behind the final sigma rule, and the data of
unicodedata.normalize('NFKC')) is read back from the interpreter itself, so the tables follow its Unicode version (15.0.0
in CPython 3.12). Run with the pinned CPython 3.12 from this directory:

    devtools/py.sh gold/internal/textutil python3 gen.py
"""

import platform
import unicodedata

ALL = range(0x110000)


def ranges(pred):
    """The inclusive (lo, hi) runs of code points for which pred is true."""
    out = []
    for c in ALL:
        if pred(chr(c)):
            if out and out[-1][1] == c - 1:
                out[-1][1] = c
            else:
                out.append([c, c])
    return out


def is_case_ignorable(ch):
    """The Case_Ignorable property, which the interpreter does not expose: it
    is read back from the final sigma rule of str.lower(). A case-ignorable
    character between a cased one and a final capital sigma is skipped; a
    cased one followed by the sigma makes it final on its own."""
    after_cased = ('a' + ch + 'Σ').lower()[-1] == 'ς'
    alone = (ch + 'Σ').lower()[-1] == 'ς'
    return after_cased and not alone


def case_runs(mapping):
    """Split a case mapping into delta runs and multi-character exceptions.

    Each run (lo, hi, stride, delta) says that lo, lo+stride, ..., hi map to
    themselves plus delta; runs take consecutive mapped code points, so they
    never overlap. Code points whose mapping has several characters go to the
    exception table."""
    runs, special = [], {}
    for c in ALL:
        m = mapping(chr(c))
        if m == chr(c):
            continue
        if len(m) > 1:
            special[c] = m
            continue
        d = ord(m) - c
        if runs:
            lo, hi, stride, delta = runs[-1]
            step = c - hi
            if delta == d and (stride == step or (lo == hi and step <= 2)):
                runs[-1] = [lo, c, step, d]
                continue
        runs.append([c, c, 1, d])
    return runs, special


def go_char(ch):
    if 0x20 <= ord(ch) < 0x7f and ch not in '"\\':
        return ch
    if ord(ch) > 0xffff:
        return '\\U%08x' % ord(ch)
    return '\\u%04x' % ord(ch)


def go_string(s):
    return '"' + ''.join(go_char(ch) for ch in s) + '"'


def emit_items(out, items):
    line = '\t'
    for item in items:
        if len(line) + len(item) > 110:
            out.append(line.rstrip())
            line = '\t'
        line += item + ' '
    if line.strip():
        out.append(line.rstrip())


def emit_ranges(out, name, doc, rs):
    out.append('// %s %s' % (name, doc))
    out.append('var %s = []runeRange{' % name)
    emit_items(out, ['{0x%x, 0x%x},' % (lo, hi) for lo, hi in rs])
    out.append('}')
    out.append('')


def emit_mapping(out, name, method):
    runs, special = case_runs(getattr(str, method))
    out.append('// %sRuns holds the single-character results of str.%s().' % (name, method))
    out.append('var %sRuns = []caseRun{' % name)
    emit_items(out, ['{0x%x, 0x%x, %d, %d},' % tuple(r) for r in runs])
    out.append('}')
    out.append('')
    out.append('// %sSpecial holds the results of str.%s() that have several characters.'
               % (name, method))
    out.append('var %sSpecial = map[rune]string{' % name)
    emit_items(out, ['0x%x: %s,' % (c, go_string(special[c])) for c in sorted(special)])
    out.append('}')
    out.append('')


HANGUL = range(0xAC00, 0xAC00 + 11172)


def emit_normalization(out):
    """The data of NFKC: the full compatibility decompositions (Hangul
    syllables, which decompose by arithmetic, left out), the canonical
    combining classes, and the primary composites with the pairs they
    compose from."""
    decomp = {c: unicodedata.normalize('NFKD', chr(c)) for c in ALL
              if c not in HANGUL and unicodedata.normalize('NFKD', chr(c)) != chr(c)}
    out.append('// compatDecompositions holds the full compatibility decomposition (NFKD) of')
    out.append('// every character that has one, the Hangul syllables excepted.')
    out.append('var compatDecompositions = map[rune]string{')
    emit_items(out, ['0x%x: %s,' % (c, go_string(decomp[c])) for c in sorted(decomp)])
    out.append('}')
    out.append('')
    runs = []
    for c in ALL:
        k = unicodedata.combining(chr(c))
        if not k:
            continue
        if runs and runs[-1][1] == c - 1 and runs[-1][2] == k:
            runs[-1][1] = c
        else:
            runs.append([c, c, k])
    out.append('// combiningClasses holds the canonical combining classes other than 0.')
    out.append('var combiningClasses = []combiningRun{')
    emit_items(out, ['{0x%x, 0x%x, %d},' % tuple(r) for r in runs])
    out.append('}')
    out.append('')
    pairs = {}
    for c in ALL:
        if c in HANGUL:
            continue
        d = unicodedata.decomposition(chr(c)).split()
        if len(d) == 2 and not d[0].startswith('<'):
            a, b = int(d[0], 16), int(d[1], 16)
            if unicodedata.normalize('NFC', chr(a) + chr(b)) == chr(c):
                pairs[(a, b)] = c
    out.append('// compositions maps the pairs of characters that compose canonically to')
    out.append('// their primary composite, the Hangul syllables excepted.')
    out.append('var compositions = map[[2]rune]rune{')
    emit_items(out, ['{0x%x, 0x%x}: 0x%x,' % (a, b, pairs[(a, b)]) for a, b in sorted(pairs)])
    out.append('}')
    out.append('')


def main():
    zeros = [c for c in ALL if chr(c).isdecimal() and unicodedata.decimal(chr(c)) == 0]
    for c in ALL:
        if chr(c).isdecimal():
            z = max(x for x in zeros if x <= c)
            assert c - z < 10 and unicodedata.decimal(chr(c)) == c - z, hex(c)

    out = [
        '// Code generated by gen.py from CPython %s (Unicode %s). DO NOT EDIT.'
        % (platform.python_version(), unicodedata.unidata_version),
        '',
        'package textutil',
        '',
    ]
    emit_ranges(out, 'decimalRanges', 'holds the characters for which str.isdecimal() is true.',
                ranges(str.isdecimal))
    emit_ranges(out, 'digitRanges', 'holds the characters for which str.isdigit() is true.',
                ranges(str.isdigit))
    emit_ranges(out, 'numericRanges', 'holds the characters for which str.isnumeric() is true.',
                ranges(str.isnumeric))
    emit_ranges(out, 'alphaRanges', 'holds the characters for which str.isalpha() is true.',
                ranges(str.isalpha))
    emit_ranges(out, 'alnumRanges', 'holds the characters for which str.isalnum() is true.',
                ranges(str.isalnum))
    emit_ranges(out, 'spaceRanges', 'holds the characters for which str.isspace() is true.',
                ranges(str.isspace))
    emit_ranges(out, 'printableRanges', 'holds the characters for which str.isprintable() is true.',
                ranges(str.isprintable))
    emit_ranges(out, 'upperRanges', 'holds the Uppercase characters (str.isupper()).',
                ranges(str.isupper))
    emit_ranges(out, 'lowerRanges', 'holds the Lowercase characters (str.islower()).',
                ranges(str.islower))
    emit_ranges(out, 'titleRanges', 'holds the titlecase letters (category Lt).',
                ranges(lambda ch: ch.istitle() and not ch.isupper()))
    emit_ranges(out, 'casedRanges', 'holds the Cased characters: Uppercase, Lowercase and Lt.',
                ranges(lambda ch: ch.isupper() or ch.islower() or ch.istitle()))
    emit_ranges(out, 'caseIgnorableRanges', 'holds the Case_Ignorable characters.',
                ranges(is_case_ignorable))
    emit_ranges(out, 'xidStartRanges', 'holds the XID_Start characters.',
                ranges(lambda ch: ch != '_' and ch.isidentifier()))
    emit_ranges(out, 'xidContinueRanges', 'holds the XID_Continue characters.',
                ranges(lambda ch: ('a' + ch).isidentifier()))
    out.append('// decimalZeros holds the decimal digits whose value is 0: every decimal digit')
    out.append('// follows the zero of its run of ten.')
    out.append('var decimalZeros = []rune{')
    emit_items(out, ['0x%x,' % z for z in zeros])
    out.append('}')
    out.append('')
    emit_mapping(out, 'lower', 'lower')
    emit_mapping(out, 'upper', 'upper')
    emit_mapping(out, 'title', 'title')
    emit_mapping(out, 'fold', 'casefold')
    emit_normalization(out)
    with open('unicode_tables.go', 'w', encoding='ascii', newline='\n') as f:
        f.write('\n'.join(out).rstrip('\n') + '\n')


if __name__ == '__main__':
    main()
