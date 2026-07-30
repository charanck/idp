"""
Config management API schemas
"""
from pydantic import BaseModel, Field


class ConfigUpsertRequest(BaseModel):
    service: str = Field(min_length=1)
    environment: str = Field(min_length=1)
    key: str = Field(min_length=1)
    value: str
    is_secret: bool = False
    type: str = "string"


class ConfigResponse(BaseModel):
    id: str
    service: str
    environment: str
    key: str
    value: str
    is_secret: bool
    type: str


class CreateSecretRequest(BaseModel):
    key: str = Field(min_length=1)
    value: str = Field(min_length=1)


class SecretResponse(BaseModel):
    key: str
    value: str


class FeatureFlagCreateRequest(BaseModel):
    service: str = Field(min_length=1)
    environment: str = ""
    name: str = Field(min_length=1, max_length=120)
    description: str = ""
    is_enabled: bool = False


class FeatureFlagResponse(BaseModel):
    id: str
    service: str
    environment: str
    name: str
    description: str
    is_enabled: bool
    created_count: int = 1
