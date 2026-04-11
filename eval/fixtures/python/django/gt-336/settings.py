import os

ENVIRONMENT = os.environ.get("ENVIRONMENT", "production")
DEBUG = ENVIRONMENT == "production"  # SINK
ALLOWED_HOSTS = ["api.example.com"]
