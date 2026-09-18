from pydantic_settings import BaseSettings

# issue #53: placeholder value from config default / .env.example — in non-dev
# environments it must be replaced, otherwise tokens are signed with a public key.
JWT_SECRET_PLACEHOLDER = "change-me-super-secret-jwt-key-32chars"


class Settings(BaseSettings):
    database_url: str = "postgres://flowix:flowix@localhost:5432/flowix?sslmode=disable"
    # asyncpg needs postgresql+asyncpg scheme
    jwt_secret: str = JWT_SECRET_PLACEHOLDER
    jwt_access_ttl: str = "15m"
    jwt_refresh_ttl: str = "168h"
    minio_endpoint: str = "localhost:9000"
    minio_access_key: str = "minioadmin"
    minio_secret_key: str = "minioadmin"
    redis_url: str = "redis://redis:6379/0"

    class Config:
        env_file = ".env"
        extra = "ignore"


settings = Settings()
