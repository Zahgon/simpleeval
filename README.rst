simpleeval (Simple Eval) for Go
===============================

A library for easily adding evaluatable expressions into Go programs: a Go
port of the Python ``simpleeval`` library (version 1.0.8). Say you want to
allow a user to set an alarm volume, which could depend on the time of day,
alarm level, how many previous alarms had gone off, and if there is music
playing at the time. Or you want to allow simple formulae in a web
application, without giving away a whole interpreter.

Expressions are Python 3.12 expressions. They are parsed the way CPython's
``ast.parse`` parses them and evaluated node by node with Python's semantics:
Python's numbers (ints of any size, floats, complex numbers), strings, bytes,
lists, tuples, dicts and sets, their operators and methods, string formatting,
and the exact exception every operation raises. Only the operators, functions
and names an evaluator is given are available, so it should be safe in terms
of what operations an expression can perform.

The one issue to be aware of is that an expression can take a long time to
evaluate, or need a lot of memory, which leaves the potential for denial of
service. There is basic protection against this (see Limits_ below), and you
can lock it down further. Be aware of this when deploying in a public setting.

The module has no dependencies.

Basic usage
-----------

.. code-block:: go

    import "github.com/danthedeckie/simpleeval"

    v, err := simpleeval.Eval("21 + 21", nil, nil, nil, nil)
    s, _ := simpleeval.Repr(v) // "42"

``Eval(expr, operators, functions, names, allowedAttrs)`` is the original's
``simple_eval``: a nil argument means the default. Every error that stands for
a Python exception is a ``*simpleeval.Error`` with the exception's ``Class``
(such as ``"NameNotDefined"`` or ``"ZeroDivisionError"``) and ``Message``.

Expressions can be as complex and convoluted as you want:

.. code-block:: go

    simpleeval.Eval("21 + 19 / 7 + (8 % 3) ** 9", nil, nil, nil, nil) // 535.7142857142857

Values
------

Values are Python values (``simpleeval.Value``). Build the values you hand in
with ``None``, ``Bool``, ``Int``, ``BigInt``, ``Float``, ``Complex``,
``String``, ``Bytes``, ``List``, ``Tuple``, ``Set``, ``FrozenSet`` and
``Dict``; ``Builtin("len")`` is a Python builtin and ``Module("math")`` a
standard-library module. ``Repr``, ``Str``, ``GetAttr``, ``GetItem``,
``SetItem`` and ``Call`` are Python's operations on them.

Names and functions
-------------------

.. code-block:: go

    names, _ := simpleeval.Dict(simpleeval.String("two"), simpleeval.Int(2))
    simpleeval.Eval("40 + two", nil, nil, names, nil) // 42

    magnitude := simpleeval.Func("magnitude", func(args []simpleeval.Value, kw []simpleeval.Keyword) (simpleeval.Value, error) {
        return simpleeval.Call(simpleeval.Builtin("abs"), args, nil)
    })
    functions := simpleeval.DefaultFunctions()
    simpleeval.SetItem(functions, simpleeval.String("magnitude"), magnitude)
    simpleeval.Eval("magnitude(-3) + 1", nil, functions, nil, nil) // 4

The names may also be a function, called with the ``ast.Name`` node of each
name the expression uses; it returns ``NewNameNotDefined(name, expression)``
for a name it does not know, and the evaluator then looks the name up among
its functions.

The default functions are ``rand``, ``randint``, ``int``, ``float`` and
``str``; the default names are ``True``, ``False`` and ``None``.

Evaluators
----------

``NewSimpleEval(operators, functions, names, allowedAttrs)`` makes an
evaluator you can keep and reuse; its ``Operators``, ``Functions``, ``Names``,
``AllowedAttrs``, ``AttrIndexFallback`` and ``Expr`` fields can be changed
between evaluations. ``Parse`` parses an expression once, and ``EvalParsed``
evaluates the parsed node again.

``NewEvalWithCompoundTypes`` adds list, tuple, dict and set displays, the
``list``, ``tuple``, ``dict`` and ``set`` functions, list and dict
comprehensions and generator expressions (evaluated to lists).

Operators
---------

``DefaultOperators()`` maps the class name of each operator node (``Add``,
``USub``, ``Eq``, ``In``, ...) to the function that applies it. ``+``, ``*``,
``**``, ``<<`` and ``>>`` use guarded functions that refuse results that would
be too large. Remove an operator from the map to disallow it, or replace its
function.

Limits
------

The package variables ``MaxStringLength``, ``MaxComprehensionLength``,
``MaxPower``, ``MaxShift`` and ``MaxShiftBase`` bound string literals and
sequence arithmetic, comprehension results, the operands of ``**`` and of the
shifts. They are read whenever they are checked, so changing one affects every
evaluator.

Attributes
----------

Attributes starting with ``_`` or ``func_`` (``DisallowPrefixes``) and the
methods in ``DisallowMethods`` are never reachable. An attribute that does not
exist falls back to an item of the same name (``d.key`` for ``d["key"]``)
unless ``AttrIndexFallback`` is false. An evaluator given ``AllowedAttrs``
(``BasicAllowedAttrs`` is a sensible start) lets expressions reach only the
listed attributes of the listed types.

Modules are refused; ``NewModuleWrapper(module, allowedAttrs)`` exposes one
explicitly, still refusing private names and the disallowed methods.

The functions in ``DisallowFunctions`` (``eval``, ``exec``, ``type``,
``getattr``, ``open``, the ``os`` functions that run programs, ...) are
refused wherever they appear.

Warnings
--------

Assignments are evaluated for their value with an ``AssignmentAttempted``
warning, and only the first of several statements is evaluated, with a
``MultipleExpressions`` warning. ``CatchWarnings`` records the warnings issued
while a function runs.

Credits
-------

simpleeval was written by Daniel Fairhead, with contributions from many
people (see the original project's contributors list). This port follows its
behaviour, and that of CPython 3.12, as closely as it can.

Licence
-------

MIT, see ``LICENCE``. Parts of the library are ported from CPython and carry
the Python Software Foundation's licence notice in their source files.
