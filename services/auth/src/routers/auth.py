from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from ..core.db import get_db
from ..core.security import (
    create_access_token,
    create_refresh_token,
    decode_token,
    hash_password,
    verify_password,
)
from ..models import User
from ..schemas import LoginRequest, RegisterRequest, TokenResponse, UserResponse

security = HTTPBearer(auto_error=False)
refresh_security = HTTPBearer(auto_error=False)


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
async def register(body: RegisterRequest, db: AsyncSession = Depends(get_db)):
    q = await db.execute(select(User).where(User.email == body.email))
    if q.scalar_one_or_none():
        raise HTTPException(409, "email already exists")
    user = User(email=body.email, password_hash=hash_password(body.password))
    db.add(user)
    await db.commit()
    await db.refresh(user)
    return TokenResponse(
        access_token=create_access_token(user.id),
        refresh_token=create_refresh_token(user.id),
    )


@router.post("/login", response_model=TokenResponse)
async def login(body: LoginRequest, db: AsyncSession = Depends(get_db)):
    q = await db.execute(select(User).where(User.email == body.email))
    user = q.scalar_one_or_none()
    if not user or not verify_password(body.password, user.password_hash):
        raise HTTPException(401, "invalid credentials")
    return TokenResponse(
        access_token=create_access_token(user.id),
        refresh_token=create_refresh_token(user.id),
    )


@router.post("/refresh", response_model=TokenResponse)
async def refresh(token: str = Depends(_get_refresh_token)):
    try:
        payload = decode_token(token)
    except ValueError:
        raise HTTPException(401, "invalid token")
    if payload.get("type") != "refresh":
        raise HTTPException(401, "not a refresh token")
    sub = payload["sub"]
    return TokenResponse(
        access_token=create_access_token(sub), refresh_token=create_refresh_token(sub)
    )


@router.get("/me", response_model=UserResponse)
async def me(token: str = Depends(_get_token), db: AsyncSession = Depends(get_db)):
    try:
        payload = decode_token(token)
    except ValueError:
        raise HTTPException(401, "invalid token")
    user_id = payload["sub"]
    q = await db.execute(select(User).where(User.id == user_id))
    user = q.scalar_one_or_none()
    if not user:
        raise HTTPException(404, "user not found")
    return UserResponse(id=user.id, email=user.email)
