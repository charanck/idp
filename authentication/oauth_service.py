"""
OAuth2/OIDC Service - Simplified
"""
import logging

from authlib.integrations.requests_client import OAuth2Session
from authlib.integrations.base_client.errors import OAuthError
from django.contrib.auth import get_user_model
from datetime import datetime, timedelta
from typing import Dict, Tuple
import requests

from .oauth_models import OAuthProvider, OAuthUserToken

User = get_user_model()

logger = logging.getLogger(__name__)


class OAuthService:
    """OAuth2/OIDC service for user authentication"""

    def get_authorization_url(self, provider: OAuthProvider, redirect_uri: str) -> Tuple[str, str]:
        """Get OAuth2 authorization URL (returns: authorization_url, state)"""
        oauth = OAuth2Session(
            client_id=provider.client_id,
            client_secret=provider.client_secret,
            scope=provider.scope,
            redirect_uri=redirect_uri
        )
        return oauth.create_authorization_url(provider.authorization_url)

    def exchange_code_for_token(self, provider: OAuthProvider, code: str, redirect_uri: str) -> Dict:
        """Exchange authorization code for access token"""
        oauth = OAuth2Session(
            client_id=provider.client_id,
            client_secret=provider.client_secret,
            redirect_uri=redirect_uri
        )
        try:
            return oauth.fetch_token(
                url=provider.token_url,
                code=code,
                grant_type='authorization_code'
            )
        except OAuthError as e:
            logger.warning("OAuth token exchange rejected by provider %s: %s", provider.name, e)
            raise ValueError(f"Provider rejected the authorization code: {e}") from e
        except requests.RequestException as e:
            logger.error("OAuth token exchange failed for provider %s: %s", provider.name, e)
            raise ValueError(f"Could not reach {provider.name} to exchange the authorization code") from e

    def get_user_info(self, provider: OAuthProvider, access_token: str) -> Dict:
        """Get user info from OAuth provider"""
        if not provider.userinfo_url:
            raise ValueError("Provider does not have userinfo_url configured")

        headers = {'Authorization': f'Bearer {access_token}'}
        try:
            response = requests.get(provider.userinfo_url, headers=headers, timeout=10)
            response.raise_for_status()
            return response.json()
        except requests.RequestException as e:
            logger.error("Failed to fetch user info from provider %s: %s", provider.name, e)
            raise ValueError(f"Could not fetch user info from {provider.name}") from e
        except ValueError as e:
            # response.json() failed to parse
            logger.error("Provider %s returned an unparseable userinfo response: %s", provider.name, e)
            raise ValueError(f"{provider.name} returned an invalid user info response") from e
    
    def authenticate_or_create_user(
        self,
        provider: OAuthProvider,
        token_data: Dict,
        user_info: Dict
    ) -> Tuple[User, OAuthUserToken]:
        """Authenticate or create user from OAuth data (returns: User, OAuthUserToken)"""
        provider_user_id = user_info.get('sub') or user_info.get('id')
        email = user_info.get('email')
        
        if not provider_user_id:
            raise ValueError("Provider did not return user ID")
        if not email:
            raise ValueError("Provider did not return email")
        
        # Check if OAuth token exists
        try:
            oauth_token = OAuthUserToken.objects.get(
                provider=provider,
                provider_user_id=provider_user_id
            )
            user = oauth_token.user
            
            # Update token
            oauth_token.access_token = token_data.get('access_token')
            oauth_token.refresh_token = token_data.get('refresh_token', '')
            oauth_token.token_type = token_data.get('token_type', 'Bearer')
            oauth_token.provider_user_email = email
            
            if 'expires_in' in token_data:
                oauth_token.expires_at = datetime.utcnow() + timedelta(seconds=token_data['expires_in'])
            
            oauth_token.save()
            
        except OAuthUserToken.DoesNotExist:
            # Get or create user
            try:
                user = User.objects.get(email=email)
            except User.DoesNotExist:
                if not provider.auto_create_users:
                    raise ValueError("User does not exist and auto-creation is disabled")
                
                # Create unique username
                username = email.split('@')[0]
                base_username = username
                counter = 1
                while User.objects.filter(username=username).exists():
                    username = f"{base_username}{counter}"
                    counter += 1
                
                user = User.objects.create(
                    email=email,
                    username=username,
                    is_active=False
                )
                user.set_unusable_password()
                user.save()
            
            # Create OAuth token
            oauth_token = OAuthUserToken.objects.create(
                user=user,
                provider=provider,
                access_token=token_data.get('access_token'),
                refresh_token=token_data.get('refresh_token', ''),
                token_type=token_data.get('token_type', 'Bearer'),
                expires_at=datetime.utcnow() + timedelta(seconds=token_data.get('expires_in', 3600)),
                provider_user_id=provider_user_id,
                provider_user_email=email
            )
        
        return user, oauth_token
