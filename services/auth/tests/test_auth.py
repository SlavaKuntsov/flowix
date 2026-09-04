import uuid
from unittest.mock import AsyncMock, MagicMock

from fastapi.testclient import TestClient

from src.core.db import get_db
from src.core.security import create_access_token, create_refresh_token, hash_password
from src.main import app


class FakeResult:
    def __init__(self, obj):
        self._obj = obj

    def scalar_one_or_none(self):
        return self._obj


def fake_user(email="test@example.com", password="secret123"):
    u = MagicMock()
    u.id = uuid.uuid4()
    u.email = email
    u.password_hash = hash_password(password)
    return u


def override_db(mock_db):
    async def _get_db():
        yield mock_db

    return _get_db


def client_with_mock(mock_db):
    app.dependency_overrides[get_db] = override_db(mock_db)
    c = TestClient(app)
    return c


def clear_overrides():
    app.dependency_overrides.clear()
    # reset rate-limit storage to avoid cross-test 429
    try:
        from src.core.limiter import limiter

        if limiter is not None and hasattr(limiter, "_storage"):
            try:
                limiter._storage.reset()  # type: ignore[attr-defined]
            except Exception:
                pass
    except Exception:
        pass


def test_register_success():
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(None))
    mock_db.add = MagicMock()
    mock_db.commit = AsyncMock()
    mock_db.refresh = AsyncMock()

    c = client_with_mock(mock_db)
    r = c.post("/api/v1/auth/register", json={"email": "new@example.com", "password": "secret123"})
    clear_overrides()
    assert r.status_code == 201, r.text
    data = r.json()
    assert "access_token" in data
    assert "refresh_token" in data
    mock_db.add.assert_called_once()
    mock_db.commit.assert_awaited_once()


def test_register_duplicate():
    existing = fake_user(email="dup@example.com")
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(existing))
    c = client_with_mock(mock_db)
    r = c.post("/api/v1/auth/register", json={"email": "dup@example.com", "password": "secret123"})
    clear_overrides()
    assert r.status_code == 409


def test_login_success():
    u = fake_user(email="login@example.com", password="secret123")
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(u))
    c = client_with_mock(mock_db)
    r = c.post("/api/v1/auth/login", json={"email": "login@example.com", "password": "secret123"})
    clear_overrides()
    assert r.status_code == 200
    assert "access_token" in r.json()


def test_login_invalid_password():
    u = fake_user(email="login@example.com", password="secret123")
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(u))
    c = client_with_mock(mock_db)
    r = c.post("/api/v1/auth/login", json={"email": "login@example.com", "password": "wrong"})
    clear_overrides()
    assert r.status_code == 401


def test_login_unknown_email():
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(None))
    c = client_with_mock(mock_db)
    r = c.post("/api/v1/auth/login", json={"email": "nope@example.com", "password": "secret123"})
    clear_overrides()
    assert r.status_code == 401


def test_me_success():
    u = fake_user(email="me@example.com")
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(u))
    token = create_access_token(str(u.id))
    c = client_with_mock(mock_db)
    r = c.get("/api/v1/auth/me", headers={"Authorization": f"Bearer {token}"})
    clear_overrides()
    assert r.status_code == 200, r.text
    assert r.json()["email"] == "me@example.com"


def test_me_missing_token():
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(None))
    c = client_with_mock(mock_db)
    r = c.get("/api/v1/auth/me")
    clear_overrides()
    assert r.status_code == 401


def test_me_invalid_token():
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(None))
    c = client_with_mock(mock_db)
    r = c.get("/api/v1/auth/me", headers={"Authorization": "Bearer invalid"})
    clear_overrides()
    assert r.status_code == 401


def test_refresh_success():
    uid = str(uuid.uuid4())
    refresh = create_refresh_token(uid)
    c = TestClient(app)
    r = c.post("/api/v1/auth/refresh", headers={"Authorization": f"Bearer {refresh}"})
    assert r.status_code == 200, r.text
    assert "access_token" in r.json()


def test_refresh_with_access_token_rejected():
    uid = str(uuid.uuid4())
    access = create_access_token(uid)
    c = TestClient(app)
    r = c.post("/api/v1/auth/refresh", headers={"Authorization": f"Bearer {access}"})
    assert r.status_code == 401


def test_health():
    c = TestClient(app)
    r = c.get("/health")
    assert r.status_code == 200
    assert r.json()["status"] == "ok"


def test_login_rate_limit():
    u = fake_user(email="ratelimit@example.com", password="secret123")
    mock_db = AsyncMock()
    mock_db.execute = AsyncMock(return_value=FakeResult(u))
    c = client_with_mock(mock_db)
    # reset limiter before test
    try:
        from src.core.limiter import limiter as _lim

        if _lim is not None and hasattr(_lim, "_storage"):
            _lim._storage.reset()  # type: ignore[attr-defined]
    except Exception:
        pass
    for _ in range(5):
        r = c.post("/api/v1/auth/login", json={"email": "ratelimit@example.com", "password": "secret123"})
        assert r.status_code == 200, r.text
    r = c.post("/api/v1/auth/login", json={"email": "ratelimit@example.com", "password": "secret123"})
    assert r.status_code == 429, r.text
    clear_overrides()
