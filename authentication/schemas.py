"""
Authentication API schemas
"""
from pydantic import BaseModel, Field
from typing import Literal


SupportedUserAuth = Literal["oauth2_password"]


class RegisterUserRequest(BaseModel):
    email: str
    password: str = Field(min_length=8)


class UserResponse(BaseModel):
    id: str
    email: str
    username: str
    is_active: bool


class TokenRequest(BaseModel):
    username: str  # Email
    password: str


class TokenResponse(BaseModel):
    access_token: str
    token_type: str = "bearer"


class CreateServiceClientRequest(BaseModel):
    name: str = Field(min_length=3, max_length=120)


class ServiceClientResponse(BaseModel):
    id: str
    name: str
    client_id: str
    api_key_id: str
    api_key: str
