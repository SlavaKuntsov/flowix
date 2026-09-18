from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    database_url: str = "postgres://flowix:flowix@localhost:5432/flowix?sslmode=disable"
    # asyncpg needs postgresql+asyncpg scheme
    # issue #53: empty secret fail-fasts at startup (main.validate_secrets) unless ENV=dev
    jwt_secret: str = ""
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
