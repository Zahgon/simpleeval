"""Record the expectations of recorded_test.go from the original simpleeval.

Usage: python record.py > recorded.json   (on CPython 3.12 with the original simpleeval importable)

Each entry evaluates one expression with EvalWithCompoundTypes (or SimpleEval when "simple" is set),
given the default functions plus abs, len, max, min, sorted and sum and the names below, and records
repr() of the result or the exception's class and message, and the warnings issued.
"""
import json
import math
import sys
import warnings

import simpleeval

EXPRESSIONS = [
    # arithmetic and numbers
    '21 + 21', '21 + 19 / 7 + (8 % 3) ** 9', '7 // -2', '-7 % 3', '2 ** 100', '2 ** -2', '(-8) ** (1 / 3)',
    '0.1 + 0.2', '1e16', '1e-5', '10 / 4', '1 / 0', '1 % 0', '1.0 // 0', '3 * 1j ** 2', '~5', '5 & 3 | 8 ^ 2',
    '1 << 10', '1 << 10001', '2 ** 4000001', '1e300 ** 2', '10 ** 4300', 'int("0x1f", 0)', 'float(" -inf ")',
    'int("12abc")', 'float("1e")', 'str(1e16)', '(255).to_bytes(2, "little")', 'float.fromhex("0x1.8p1")',
    '(0.1).hex()', '(2.5).as_integer_ratio()', '(-5).bit_count()', 'int.from_bytes(b"\\x01\\x00", "big")',
    # strings and formatting
    '"abc" + "def"', '"ab" * 3', '"ab" * "cd"', '"ß".upper()', '"ǅ".title()', '"a b  c".split()',
    '"x\\ty".expandtabs(4)', '"-42".zfill(6)', '"abc".translate(str.maketrans("ab", "xy"))',
    '"%5.2f|%-4d|%r" % (3.14159, 7, "x")', 'b"%s-%d" % (b"x", 5)', 'f"{12345.678:,.2f}"', 'f"{255:#x}"',
    'f"{1!r:>4}"', 'f"{\'a\'=}"', 'f"{3:{\'}\'}}"', '"{:>5}".format(1)', '"abc"[::-1]', '"abc"[5]',
    '"\\d"', '"\\N{EM DASH}"',
    # comparisons and logic
    '1 < 2 < 3', '1 < "a"', '[1, 2] < [1, 3]', '1 == 1.0 == True', '"a" in "abc"', '1 in "abc"',
    'None is None', '() is ()', '0 or "" or []', '1 and 0 or 2', 'not []', 'x if x else "empty"',
    # compound types and comprehensions
    '[1, 2, 3][1:]', '{1: "a", 1.0: "b", True: "c"}', '{33, 1, 65, 9}', '{100, 200, 300, 400, 500}.copy()',
    '{1, 2, 3} - {2}', '[x * 2 for x in [1, 2, 3] if x > 1]', '{k: v for k, v in [("a", 1), ("b", 2)]}',
    '(x for x in "ab")', '{x for x in [1]}', '[a for a, b in [(1, 2, 3)]]', '[a for *a, b in [(1, 2)]]',
    '{a: b}', '{**[(1, 2)]}', '[*"ab", *x]', 'sorted([3, 1, 2], reverse=True)', 'sum([1, 2, 3])',
    'max([])', 'len(x)', 'abs(-2, 3)',
    # names, attributes and calls
    'x', 'missing', 'x.append', 'd.key', 'd.nothing', 'x.__class__', '"a".format', 'len.func_name',
    'f(1)(2)', 'nope()', 'len(**{"a": 1})', 'len(*x)', 'int | str', 'm.sqrt(2)', 'm.floor(-2.5)',
    'm.sqrt(-1)', 'm.__name__', 'm.nope',
    # statements and parsing
    'x = 1', '1; 2', '', '# comment', 'pass', 'import os', 'lambda: 1', '(y := 1)', 'del x', '(1 + 2',
    '1 +', 'f(a b)', '1 = 2', '09', "'abc", 'if x:',
]
NAMES = {'x': [1, 2], 'd': {'key': 'value'}, 'm': simpleeval.ModuleWrapper(math)}
SIMPLE = {'"abc"[::-1]', '1 < 2 < 3', 'x', 'd.key'}


def outcome(expr):
    functions = {**simpleeval.DEFAULT_FUNCTIONS, 'abs': abs, 'len': len, 'max': max, 'min': min,
                 'sorted': sorted, 'sum': sum}
    cls = simpleeval.SimpleEval if expr in SIMPLE else simpleeval.EvalWithCompoundTypes
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter('always')
        try:
            result = cls(functions=functions, names=dict(NAMES)).eval(expr)
            text = '= ' + repr(result)
        except Exception as e:  # noqa: BLE001 - every exception is recorded
            text = f'! {type(e).__name__}: {e}'
    return {'expr': expr, 'simple': expr in SIMPLE, 'outcome': text,
            'warnings': [f'{w.category.__name__}: {w.message}' for w in caught]}


json.dump([outcome(e) for e in EXPRESSIONS], sys.stdout, ensure_ascii=False, indent=1)
sys.stdout.write('\n')
