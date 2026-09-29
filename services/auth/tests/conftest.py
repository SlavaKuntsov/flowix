import pytest

from src.core.config import settings
from src.core.tokenstore import get_token_store
from src.main import app

TEST_JWT_SECRET = "test-secret-for-auth-unit-tests"


class FakeTokenStore:
    """In-memory замена TokenStore: issue/rotate с reuse-детекцией (issue #64)."""

    def __init__(self):
        self.data: dict[str, str] = {}
        self.fail = False

    async def issue(self, user_id: str, jti: str, ttl_seconds: int) -> None:
        if self.fail:
            from src.core.tokenstore import RedisError

            raise RedisError("store down")
        self.data[user_id] = jti

    async def rotate(self, user_id: str, old_jti: str, new_jti: str, ttl_seconds: int) -> bool:
        if self.fail:
            from src.core.tokenstore import RedisError

            raise RedisError("store down")
        if self.data.get(user_id) != old_jti:
            return False
        self.data[user_id] = new_jti
        return True


@pytest.fixture(autouse=True)
def fake_store():
    # Redis в юнит-тестах недоступен — все тесты получают in-memory store
    store = FakeTokenStore()
    app.dependency_overrides[get_token_store] = lambda: store
    yield store
    app.dependency_overrides.pop(get_token_store, None)


@pytest.fixture(autouse=True)
def _jwt_secret(monkeypatch):
    # In CI there is no .env, so settings.jwt_secret defaults to "" —
    # PyJWT refuses to sign HMAC with an empty key. Give every test a
    # non-empty secret; test_validate_secrets_fail_fast overrides it itself.
    if not settings.jwt_secret:
        monkeypatch.setattr(settings, "jwt_secret", TEST_JWT_SECRET)
    yield
