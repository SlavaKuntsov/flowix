from pydantic import BaseModel, EmailStr, Field, field_validator

PASSWORD_MIN_LENGTH = 8


def _normalize_email(v: str) -> str:
    # issue #49: users.email UNIQUE is case-sensitive — store and query lowercase
    return v.strip().lower()


class RegisterRequest(BaseModel):
    email: EmailStr
    password: str = Field(min_length=PASSWORD_MIN_LENGTH)

    @field_validator("email")
    @classmethod
    def email_lowercase(cls, v: str) -> str:
        return _normalize_email(v)


class LoginRequest(BaseModel):
    email: EmailStr
    password: str

    @field_validator("email")
    @classmethod
    def email_lowercase(cls, v: str) -> str:
        return _normalize_email(v)


class TokenResponse(BaseModel):
    access_token: str
    refresh_token: str
    token_type: str = "bearer"


class UserResponse(BaseModel):
    id: str
    email: str
