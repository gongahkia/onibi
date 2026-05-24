import ldap
from flask import request

def handler(conn):
    return conn.search_s("dc=example,dc=com", ldap.SCOPE_SUBTREE, f"(uid={request.form['username']})")
