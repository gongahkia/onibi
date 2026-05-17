import hashlib

token = hashlib.md5(user.encode()).hexdigest()
