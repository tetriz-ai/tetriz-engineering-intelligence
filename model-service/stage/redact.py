# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

"""Scrub credentials and identity out of text before it reaches the model provider.

The stage classifier sends artifact descriptions and bodies — skill, command and agent files an
organisation wrote — to a third-party model. Each one passes through ``redact`` first.

This is a bounded-risk reducer, not a proof of absence. A bare internal hostname matches nothing
(only credentials in a URL's userinfo do), and a high-entropy secret with no recognisable prefix or
assignment around it is invisible to a structural pattern set.
"""

from __future__ import annotations

import re

# What replaces a matched secret: fixed-width and content-free.
REDACTION_PLACEHOLDER = "[redacted]"

# Most specific first; anchored on structure rather than on a vendor name where possible.
SECRET_PATTERN_SOURCES: tuple[str, ...] = (
    # URL userinfo — strip credentials before the host survives.
    r"(?i)\b([a-z][a-z0-9+.\-]*://)[^\s/@:]+:[^\s/@]+@",
    # Authorization headers and bearer tokens.
    r"(?i)\b(authorization\s*[:=]\s*)\S+",
    r"(?i)\bbearer\s+[A-Za-z0-9._\-]{8,}",
    # Common provider key shapes: a recognizable prefix plus a long opaque body.
    r"\b(?:sk|pk|rk|ak)-[A-Za-z0-9_\-]{16,}",
    r"\bgh[pousr]_[A-Za-z0-9]{16,}",
    r"\bxox[abposr]-[A-Za-z0-9\-]{10,}",
    r"\bAKIA[0-9A-Z]{12,}",
    # PEM private key headers.
    r"(?i)-{2,}\s*BEGIN[^-]{0,40}PRIVATE KEY\s*-{2,}",
    # Explicit secret-ish assignments: key=value where the key names a credential.
    r"(?i)\b(?:api[_\-]?key|secret|password|passwd|token|credential)s?\s*[:=]\s*\S+",
    # JWTs: three base64url segments.
    r"\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}",
    # Email addresses — identity, not a secret, but not ours to hand to a provider.
    r"\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b",
)
_SECRET_PATTERNS = tuple(re.compile(p) for p in SECRET_PATTERN_SOURCES)

# A home directory through its account-name segment becomes "~": only the account name is
# sensitive. No trailing separator is required ("cd /Users/jsmith" is caught), and the account
# segment must contain a non-digit ("GET /Users/42/orders" is left alone).
_HOME_PATH = re.compile(r"(?:/Users|/home)/[^/\s]*[^/\s\d][^/\s]*")


def redact(text: str) -> str:
    """``text`` with every matched secret replaced and home directories shortened.

    Whitespace and length are kept: the classifier reads the whole definition.
    """
    if not text:
        return ""
    out = text
    for pattern in _SECRET_PATTERNS:
        out = pattern.sub(REDACTION_PLACEHOLDER, out)
    return _HOME_PATH.sub("~", out)
