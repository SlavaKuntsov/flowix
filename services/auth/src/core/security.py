import re
from datetime import datetime, timedelta, timezone

import jwt
from argon2 import PasswordHasher
from argon2.exceptions import Argon2Error

from .config import settings

# Issue #48: python-jose CVE-2024-33664 (JWT bomb DoS in decode) — reject
# oversized tokens before parsing instead of relying on the lib.
MAX_TOKEN_LENGTH = 8192

pwd_context = PasswordHasher()
ALGO = "HS256"


def hash_password(pw: str) -> str:
    return pwd_context.hash(pw)


def verify_password(plain: str, hashed: str) -> bool:
    try:
        return pwd_context.verify(hashed, plain)
    except Argon2Error:
        return False


def _parse_ttl(s: str) -> timedelta:
    m = re.match(r"^(\d+)([smhd])$", s.strip())
    if not m:
        return timedelta(minutes=15)
    n, unit = int(m.group(1)), m.group(2)
    return {
        "s": timedelta(seconds=n),
        "m": timedelta(minutes=n),
        "h": timedelta(hours=n),
        "d": timedelta(days=n),
    }[unit]


def create_access_token(sub: str) -> str:
    exp = datetime.now(timezone.utc) + _parse_ttl(settings.jwt_access_ttl)
    return jwt.encode(
        {"sub": sub, "exp": exp, "type": "access"}, settings.jwt_secret, algorithm=ALGO
    )


def create_refresh_token(sub: str) -> str:
    exp = datetime.now(timezone.utc) + _parse_ttl(settings.jwt_refresh_ttl)
    return jwt.encode(
        {"sub": sub, "exp": exp, "type": "refresh"}, settings.jwt_secret, algorithm=ALGO
    )


def decode_token(token: str) -> dict:
    if len(token) > MAX_TOKEN_LENGTH:
        raise ValueError("token too large")
    try:
        return jwt.decode(
            token,
            settings.jwt_secret,
            algorithms=[ALGO],
            options={"require": ["exp", "sub"]},
        )
    except jwt.PyJWTError as e:
        raise ValueError(str(e))
