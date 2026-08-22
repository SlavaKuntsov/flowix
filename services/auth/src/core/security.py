import re
from datetime import datetime, timedelta, timezone
from jose import jwt, JWTError  # type: ignore[import-untyped]
from passlib.context import CryptContext  # type: ignore[import-untyped]
from .config import settings

pwd_context = CryptContext(schemes=["argon2"], deprecated="auto")
ALGO = "HS256"


def hash_password(pw: str) -> str:
    return pwd_context.hash(pw)


def verify_password(plain: str, hashed: str) -> bool:
    return pwd_context.verify(plain, hashed)


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
    try:
        return jwt.decode(token, settings.jwt_secret, algorithms=[ALGO])
    except JWTError as e:
        raise ValueError(str(e))
