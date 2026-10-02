import uuid

from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncSession

from ..core.db import get_db
from ..core.limiter import limiter
from ..core.security import (
    create_access_token,
    create_refresh_token,
    decode_token,
    hash_password,
    refresh_ttl_seconds,
    verify_password,
)
from ..core.tokenstore import RedisError, TokenStore, get_token_store
from ..models import User
from ..schemas import LoginRequest, RegisterRequest, TokenResponse, UserResponse

security = HTTPBearer(auto_error=False)
refresh_security = HTTPBearer(auto_error=False)


def _limited(limit: str):  # type: ignore[no-untyped-def]
    # limiter None (slowapi недоступен) → маршрут без лимита, как login без _lim
    def deco(fn):  # type: ignore[no-untyped-def]
        if limiter is not None:
            return limiter.limit(limit)(fn)
        return fn

    return deco


def _get_token(
    creds: HTTPAuthorizationCredentials | None = Depends(security),
) -> str:
    if not creds:
        raise HTTPException(401, "missing token")
    if creds.scheme.lower() != "bearer":
        raise HTTPException(401, "invalid auth scheme")
    return creds.credentials


def _get_refresh_token(
    creds: HTTPAuthorizationCredentials | None = Depends(refresh_security),
) -> str:
    if not creds:
        raise HTTPException(401, "missing token")
    if creds.scheme.lower() != "bearer":
        raise HTTPException(401, "invalid auth scheme")
    return creds.credentials


router = APIRouter(prefix="/api/v1/auth", tags=["auth"])


@router.post("/register", response_model=TokenResponse, status_code=201)
@_limited("5/minute")
async def register(
    request: Request,
    body: RegisterRequest,
    db: AsyncSession = Depends(get_db),
    store: TokenStore = Depends(get_token_store),
):
    q = await db.execute(select(User).where(User.email == body.email))
    if q.scalar_one_or_none():
        raise HTTPException(409, "email already exists")
    user = User(email=body.email, password_hash=hash_password(body.password))
    db.add(user)
    try:
        await db.commit()
    except IntegrityError:
        # конкурентная регистрация той же почты (issue #49)
        await db.rollback()
        raise HTTPException(409, "email already exists")
    await db.refresh(user)
    jti = uuid.uuid4().hex
    try:
        await store.issue(str(user.id), jti, refresh_ttl_seconds())
    except RedisError:
        raise HTTPException(503, "token store unavailable")
    return TokenResponse(
        access_token=create_access_token(str(user.id)),
        refresh_token=create_refresh_token(str(user.id), jti),
    )


async def _login_impl(
    request: Request,
    body: LoginRequest,
    db: AsyncSession = Depends(get_db),
    store: TokenStore = Depends(get_token_store),
):  # type: ignore[no-untyped-def]
    q = await db.execute(select(User).where(User.email == body.email))
    user = q.scalar_one_or_none()
    if not user or not verify_password(body.password, user.password_hash):
        raise HTTPException(401, "invalid credentials")
    jti = uuid.uuid4().hex
    try:
        await store.issue(str(user.id), jti, refresh_ttl_seconds())
    except RedisError:
        raise HTTPException(503, "token store unavailable")
    return TokenResponse(
        access_token=create_access_token(str(user.id)),
        refresh_token=create_refresh_token(str(user.id), jti),
    )


# Register login route with rate limit if limiter is available
try:
    from ..core.limiter import limiter as _lim  # type: ignore[import-not-found]

    if _lim is not None:
        router.post("/login", response_model=TokenResponse)(_lim.limit("5/minute")(_login_impl))  # type: ignore[attr-defined]
        # expose for tests
        login = _login_impl  # type: ignore[assignment]
    else:
        router.post("/login", response_model=TokenResponse)(_login_impl)
        login = _login_impl  # type: ignore[assignment]
except Exception:
    router.post("/login", response_model=TokenResponse)(_login_impl)
    login = _login_impl  # type: ignore[assignment]


@router.post("/refresh", response_model=TokenResponse)
@_limited("30/minute")
async def refresh(
    request: Request,
    token: str = Depends(_get_refresh_token),
    db: AsyncSession = Depends(get_db),
    store: TokenStore = Depends(get_token_store),
):
    try:
        payload = decode_token(token)
    except ValueError:
        raise HTTPException(401, "invalid token")
    if payload.get("type") != "refresh":
        raise HTTPException(401, "not a refresh token")
    sub = payload["sub"]
    jti = payload.get("jti")
    if not jti:
        # токены без jti выпускались до ротации (issue #64) — отозваны
        raise HTTPException(401, "invalid token")
    try:
        uid = uuid.UUID(sub)
    except ValueError:
        raise HTTPException(401, "invalid token")
    q = await db.execute(select(User).where(User.id == uid))
    if not q.scalar_one_or_none():
        raise HTTPException(401, "invalid token")
    new_jti = uuid.uuid4().hex
    try:
        rotated = await store.rotate(sub, jti, new_jti, refresh_ttl_seconds())
    except RedisError:
        raise HTTPException(503, "token store unavailable")
    if not rotated:
        # jti не совпал с активным: reuse старого refresh (issue #64)
        raise HTTPException(401, "refresh token revoked")
    return TokenResponse(
        access_token=create_access_token(sub), refresh_token=create_refresh_token(sub, new_jti)
    )


@router.get("/me", response_model=UserResponse)
async def me(token: str = Depends(_get_token), db: AsyncSession = Depends(get_db)):
    try:
        payload = decode_token(token)
    except ValueError:
        raise HTTPException(401, "invalid token")
    if payload.get("type") != "access":
        # issue #76: refresh-токен — не identity, как в Go-мидлварях (#53)
        raise HTTPException(401, "not an access token")
    user_id = payload["sub"]
    # user_id is str from JWT; compare as UUID
    import uuid

    try:
        uid = uuid.UUID(user_id)
    except ValueError:
        raise HTTPException(401, "invalid token")
    q = await db.execute(select(User).where(User.id == uid))
    user = q.scalar_one_or_none()
    if not user:
        raise HTTPException(404, "user not found")
    return UserResponse(id=str(user.id), email=user.email)
