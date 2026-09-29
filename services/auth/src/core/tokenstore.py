from redis.asyncio import Redis
from redis.exceptions import RedisError

from .config import settings

# issue #64: активный refresh-jti на юзера. Ротация должна быть атомарной,
# иначе два параллельных refresh с одним валидным токеном оба пройдут
# (reuse-детекция теряется) — сравнение и перезапись в одной Lua-транзакции.
_ROTATE_LUA = """
local cur = redis.call('GET', KEYS[1])
if cur and cur == ARGV[1] then
  redis.call('SET', KEYS[1], ARGV[2], 'EX', tonumber(ARGV[3]))
  return 1
end
return 0
"""

_KEY_PREFIX = "auth:refresh:"


class TokenStore:
    def __init__(self, url: str):
        self._redis = Redis.from_url(url, decode_responses=True)

    async def issue(self, user_id: str, jti: str, ttl_seconds: int) -> None:
        await self._redis.set(_KEY_PREFIX + user_id, jti, ex=ttl_seconds)

    async def rotate(self, user_id: str, old_jti: str, new_jti: str, ttl_seconds: int) -> bool:
        """True — old_jti был активным и заменён; False — токен отозван/устарел."""
        result = await self._redis.eval(
            _ROTATE_LUA, 1, _KEY_PREFIX + user_id, old_jti, new_jti, ttl_seconds
        )
        return result == 1


token_store = TokenStore(settings.redis_url)


def get_token_store() -> TokenStore:
    return token_store


__all__ = ["TokenStore", "RedisError", "get_token_store", "token_store"]
