import pytest

from src.core.config import settings

TEST_JWT_SECRET = "test-secret-for-auth-unit-tests"


@pytest.fixture(autouse=True)
def _jwt_secret(monkeypatch):
    # In CI there is no .env, so settings.jwt_secret defaults to "" —
    # PyJWT refuses to sign HMAC with an empty key. Give every test a
    # non-empty secret; test_validate_secrets_fail_fast overrides it itself.
    if not settings.jwt_secret:
        monkeypatch.setattr(settings, "jwt_secret", TEST_JWT_SECRET)
    yield
