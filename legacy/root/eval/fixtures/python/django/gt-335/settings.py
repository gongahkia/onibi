import os

SECRET_KEY = os.environ.get("SECRET_KEY", "dev")
DEBUG = os.environ.get("DJANGO_DEBUG", "true").lower() == "true"  # SINK
ALLOWED_HOSTS = ["example.com"]
