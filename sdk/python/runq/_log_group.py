"""``runq.log_group`` — prefix metric keys for a block or a function.

The prefix ContextVar lives in :mod:`runq._prefix` to dodge the
``_events`` ↔ ``_log_group`` circular import.
"""
from __future__ import annotations

import functools
import inspect

from ._prefix import _prefix_stack


# ---- @log_group ---------------------------------------------------

def log_group(prefix: str):
    """Prefix all subsequent metric keys with ``prefix`` until exit.

    Dual-form API — usable BOTH as a context manager and as a
    decorator. The two forms behave identically; pick whichever reads
    cleaner at the call site::

        # Context manager
        with runq.log_group("train"):
            runq.log({"loss": l})   # key → "train/loss"

        # Decorator — plain, ``async def`` and async generator functions
        @runq.log_group("train")
        def train_one_epoch():
            runq.log({"loss": l})   # key → "train/loss"

        @runq.log_group("eval")
        async def evaluate():
            runq.log({"loss": await score()})   # key → "eval/loss"

    Nesting composes — inner prefixes append with ``/`` separator::

        with runq.log_group("train"):
            with runq.log_group("step1"):
                runq.log({"loss": l})   # → "train/step1/loss"

    Validation
    ----------
    ``prefix`` must be a non-empty string with no ``/`` characters.
    The separator is reserved so users don't accidentally write
    ``log_group("a/b")`` and shadow the nesting story.

    Thread / asyncio safety
    -----------------------
    Backed by :data:`runq._prefix._prefix_stack`, a ``ContextVar``.
    Each thread / asyncio task sees its own stack; concurrent
    ``log_group`` blocks across tasks don't bleed prefixes into each
    other.

    Implementation
    --------------
    Backed by :class:`_LogGroup`, returned fresh per ``log_group(prefix)``
    call. The class implements ``__enter__`` / ``__exit__`` /
    ``__call__`` against ``_prefix_stack`` via ``ContextVar.set`` +
    ``ContextVar.reset`` tokens. The decorator path constructs a fresh
    ``_LogGroup`` per invocation so concurrent calls don't clobber
    each other's tokens. An ``async def`` function holds the group
    across its awaits; an async generator holds it while its body runs
    (each step), not while the consumer handles a yielded item.
    """
    if not isinstance(prefix, str):
        raise TypeError(
            f"runq.log_group: prefix must be a string, got {type(prefix).__name__}"
        )
    if not prefix:
        raise ValueError("runq.log_group: prefix must be non-empty")
    if "/" in prefix:
        raise ValueError(
            "runq.log_group: prefix must not contain '/' "
            "(use nested log_group() calls for hierarchical keys)"
        )
    return _LogGroup(prefix)


class _LogGroup:
    def __init__(self, prefix):
        self._prefix = prefix
        self._token = None
    def __enter__(self):
        cur = _prefix_stack.get()
        self._token = _prefix_stack.set((*cur, self._prefix))
        return self
    def __exit__(self, exc_type, exc, tb):
        _prefix_stack.reset(self._token)
        return False
    def __call__(self, fn):
        pfx = self._prefix
        if inspect.isasyncgenfunction(fn):
            @functools.wraps(fn)
            async def agen_wrapper(*args, **kwargs):
                agen = fn(*args, **kwargs)
                step, arg = agen.asend, None
                try:
                    while True:
                        with _LogGroup(pfx):
                            try:
                                item = await step(arg)
                            except StopAsyncIteration:
                                return
                        try:
                            arg = yield item
                            step = agen.asend
                        except GeneratorExit:
                            raise
                        except BaseException as e:  # athrow() into the body
                            step, arg = agen.athrow, e
                finally:
                    with _LogGroup(pfx):
                        await agen.aclose()
            return agen_wrapper
        if inspect.iscoroutinefunction(fn):
            @functools.wraps(fn)
            async def async_wrapper(*args, **kwargs):
                with _LogGroup(pfx):
                    return await fn(*args, **kwargs)
            return async_wrapper
        @functools.wraps(fn)
        def wrapper(*args, **kwargs):
            # Fresh CM per call so concurrent invocations don't
            # share a single _token slot.
            with _LogGroup(pfx):
                return fn(*args, **kwargs)
        return wrapper
