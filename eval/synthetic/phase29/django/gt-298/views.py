from django.http import HttpResponse
from markupsafe import Markup

profiles: dict[str, str] = {}

def save_profile(request):
    user_id = request.POST["user_id"]
    submitted = request.POST["bio"]
    profiles[user_id] = submitted.strip()
    return HttpResponse("saved")

def show_profile(request):
    user_id = request.GET["user_id"]
    stored = profiles.get(user_id, "")
    card = f"<article>{stored}</article>"
    return HttpResponse(Markup(card))  # SINK
