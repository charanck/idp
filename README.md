# Control Plane - Configuration & Secret Management

A comprehensive Django + Django Ninja control plane for configuration and secret management with **enterprise-grade encryption**, user authentication, service-to-service (S2S) authentication, and a Bootstrap-based web UI.

## 🔐 **NEW: Built-in Encryption System**

All configs and secrets are now **encrypted at rest** in the database and **encrypted in transit** with client-specific encryption keys!

- ✅ Database values encrypted with master key
- ✅ Each service client gets unique encryption key  
- ✅ API serves configs/secrets encrypted for specific client
- ✅ Clients decrypt using their own keys
- ✅ Industry-standard Fernet encryption (AES-128 CBC)

See [ENCRYPTION_IMPLEMENTATION.md](./ENCRYPTION_IMPLEMENTATION.md) for detailed encryption documentation.

---

## Features

### Core Features
- **🔐 End-to-End Encryption**: Database encryption + client-specific re-encryption
- **👤 User Authentication**: JWT token-based auth with forced password reset
- **🔑 S2S Authentication**: API key-based service-to-service authentication
- **⚙️ Configuration Management**: Multi-environment, per-service configs
- **🔒 Secret Management**: Secure global secret storage
- **🚩 Feature Flags**: Toggle features with soft delete
- **🎨 Web UI**: Bootstrap 5 interface with full CRUD operations
- **📊 Dashboard**: Statistics and recent activity tracking
- **🔧 Admin Setup**: Automated admin user creation from environment

### Security Features
- **Database Encryption**: Master key encryption for all stored values
- **Client-Specific Keys**: Unique encryption key per service client
- **No Plaintext Exposure**: Values encrypted in transit and at rest
- **Bcrypt Password Hashing**: Secure password storage
- **JWT Tokens**: Configurable token expiration
- **API Key Hashing**: Secure S2S authentication
- **CSRF Protection**: For web forms
- **Force Password Reset**: Initial admin requires password change

---

## Quick Start

### Prerequisites
- Python 3.12+
- PostgreSQL (or SQLite for development)
- [uv](https://docs.astral.sh/uv/) - Fast Python package installer

### Installation

1. **Clone the repository**
```bash
git clone <repository-url>
cd control-plane
```

2. **Install uv** (if not already installed)
```bash
# On macOS/Linux
curl -LsSf https://astral.sh/uv/install.sh | sh

# On Windows
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

3. **Install dependencies with uv**
```bash
# This creates a .venv and installs all dependencies
uv sync

# Or install with dev dependencies
uv sync --group dev
```

4. **Activate the virtual environment**
```bash
# On Windows
.venv\Scripts\activate

# On macOS/Linux
source .venv/bin/activate
```

5. **Set up environment variables**
```bash
cp .env.example .env
# Edit .env with your settings
```

Required environment variables:
```env
# Database
DATABASE_URL=sqlite:///./db.sqlite3  # or PostgreSQL URL

# Security Keys
SECRET_KEY=your-django-secret-key-here
MASTER_ENCRYPTION_KEY=your-32-byte-base64-key  # Generate with: python -c "import base64, os; print(base64.urlsafe_b64encode(os.urandom(32)).decode())"

# JWT
JWT_SECRET_KEY=your-jwt-secret-key
JWT_ALGORITHM=HS256
JWT_EXPIRATION_MINUTES=60

# Admin User (for initial setup)
ADMIN_EMAIL=admin@example.com

# Optional
DEBUG=True
ALLOWED_HOSTS=localhost,127.0.0.1
```

6. **Run database migrations**
```bash
uv run python manage.py migrate
```

7. **Create admin user**
```bash
# Admin user is created automatically from ADMIN_EMAIL env var on first run
# Default password: "admin" (you'll be forced to change it on first login)
```

8. **Start the development server**
```bash
uv run python manage.py runserver
```

The application will be available at:
- **Web UI**: http://localhost:8000/
- **API Documentation**: http://localhost:8000/api/docs
- **Django Admin**: http://localhost:8000/admin/

---

## Using uv

uv is a fast Python package installer and resolver, written in Rust. It's significantly faster than pip!

### Common Commands

```bash
# Install dependencies
uv sync

# Install with dev dependencies
uv sync --group dev

# Add a new dependency
uv add package-name

# Add a dev dependency
uv add --group dev package-name

# Remove a dependency
uv remove package-name

# Update dependencies
uv sync --upgrade

# Run Python with venv
uv run python script.py

# Run Django management command
uv run python manage.py <command>

# Lock dependencies (regenerate uv.lock)
uv lock
```

### Benefits of uv
- ⚡ **10-100x faster** than pip
- 🔒 **Deterministic installs** with uv.lock
- 🎯 **Automatic virtual environment** management
- 📦 **Built-in dependency resolver**
- 🔄 **Compatible with pip** and pyproject.toml

---

```bash
# 1. Clone repository
git clone <repository-url>
cd control-plane

# 2. Create virtual environment
python -m venv venv
source venv/bin/activate  # Windows: venv\Scripts\activate

# 3. Install dependencies
pip install -r requirements.txt

# 4. Configure environment
cp .env.example .env
# Edit .env - at minimum set MASTER_ENCRYPTION_KEY!

# 5. Run migrations
python manage.py migrate

# 6. Create admin user
python manage.py setup_admin --email admin@example.com --password SecurePass123

# 7. Run server
python manage.py runserver

# 8. Access application
# Web UI: http://127.0.0.1:8000/
# API Docs: http://127.0.0.1:8000/api/docs
# Django Admin: http://127.0.0.1:8000/admin/
```

---

## 🔐 Encryption System

### How It Works

1. **Admin creates config** (JWT auth):
   ```bash
   POST /api/configs/upsert
   {
     "service": "my-app",
     "environment": "prod",
     "key": "DB_PASSWORD",
     "value": "plain-text-password"
   }
   ```
   → Encrypted with master key → Stored in database

2. **Client requests config** (API Key auth):
   ```bash
   GET /api/configs/list?service=my-app&environment=prod
   X-API-Key: <key_id>.<secret>
   ```
   → Server decrypts from database → Re-encrypts for client → Returns encrypted value

3. **Client decrypts locally**:
   ```python
   from cryptography.fernet import Fernet
   
   fernet = Fernet(client_encryption_key.encode())
   decrypted = fernet.decrypt(encrypted_value.encode()).decode()
   ```

### Generate Master Encryption Key

```bash
python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"
```

Add to `.env`:
```bash
MASTER_ENCRYPTION_KEY=<generated-key>
```

**⚠️ CRITICAL**: Keep this key secure! If lost, all data becomes unrecoverable.

---

## API Examples

### 1. Create Service Client (Admin)

```bash
curl -X POST http://127.0.0.1:8000/api/s2s/create-client \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-service", "description": "My service client"}'
```

**Response includes encryption_key** - save it securely!

### 2. Get Encrypted Configs (Client)

```bash
curl "http://127.0.0.1:8000/api/configs/list?service=my-service&environment=production" \
  -H "X-API-Key: <client-api-key>"
```

Values returned are encrypted with the client's encryption key.

### 3. Decrypt Client-Side

```python
from cryptography.fernet import Fernet

encryption_key = "client-encryption-key-from-creation"
fernet = Fernet(encryption_key.encode())
decrypted = fernet.decrypt(encrypted_value.encode()).decode()
```

---

## Documentation

- **[ENCRYPTION_IMPLEMENTATION.md](./ENCRYPTION_IMPLEMENTATION.md)** - Detailed encryption docs
- **[README_DJANGO_USAGE.md](./README_DJANGO_USAGE.md)** - Comprehensive usage guide
- **[IMPLEMENTATION_COMPLETE.md](./IMPLEMENTATION_COMPLETE.md)** - Implementation summary
- **API Docs**: http://127.0.0.1:8000/api/docs

---

## Security Best Practices

1. **Set MASTER_ENCRYPTION_KEY** in environment (never commit!)
2. **Use PostgreSQL** in production
3. **Enable HTTPS** for all production traffic
4. **Secure client encryption keys** - clients must store them safely
5. **Backup master key** securely
6. **Set DEBUG=False** in production
7. **Change default secrets** (DJANGO_SECRET_KEY, JWT_SECRET_KEY)

---

## License

MIT License

---

**Made with ❤️ using Django + Django Ninja + Cryptography**
