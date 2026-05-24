from django.http import JsonResponse
from app.models import User

def load_team(team: str):
    return User.objects.extra(where=[f"team = '{team}'"])  # SINK

def team_members(request):
    team = request.POST["team"]
    rows = load_team(team)
    return JsonResponse({"count": rows.count()})
