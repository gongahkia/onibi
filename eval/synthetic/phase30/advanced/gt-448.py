from flask import redirect, render_template_string, request

users = []

def register():
    users.append({"name": request.form["name"]})
    return redirect("/profile")

def admin_users():
    html = "".join(f"<li>{user['name']}</li>" for user in users)
    return render_template_string(f"<ul>{html}</ul>")  # sink
