from flask import request

invites = {"alpha": 1}

def claim_invite():
    code = request.form["code"]
    remaining = invites.get(code, 0)
    if remaining > 0:
        invites[code] = remaining - 1  # sink
        return "claimed"
    return "closed"
