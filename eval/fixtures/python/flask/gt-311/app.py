from flask import Flask, request, render_template_string

app = Flask(__name__)

def build_card(bio: str) -> str:
    return f"<section class='bio'>{bio}</section>"

@app.post("/profile")
def profile():
    bio = request.form["bio"]
    card = build_card(bio)
    return render_template_string(card)  # SINK
