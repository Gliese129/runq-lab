"""Public SDK exception types.

All runq SDK exceptions inherit from :class:`RunqError` so user code can
catch ``except runq.RunqError`` to handle every SDK-originated failure
uniformly. 
"""
from __future__ import annotations


class RunqError(Exception):
    """Base class for every exception the runq SDK raises.

    User code can ``except runq.RunqError`` to catch any SDK failure
    while letting unrelated exceptions (KeyboardInterrupt,
    ImportError, ...) propagate as usual.
    """
