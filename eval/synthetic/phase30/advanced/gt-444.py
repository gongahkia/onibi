from flask import request

def refund(wallet):
    amount = int(request.form["amount"])
    wallet.credit(amount)  # sink
    return "ok"
