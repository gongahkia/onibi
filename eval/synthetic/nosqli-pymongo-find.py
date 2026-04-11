from flask import request

def handler(db):
    return db.users.find(request.json)
