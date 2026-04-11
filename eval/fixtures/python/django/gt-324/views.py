from django.http import HttpResponse
from app.models import User

def build_query(team: str) -> str:
    return f"SELECT * FROM app_user WHERE team = '{team}'"

def team_view(request):
    team = request.GET["team"]
    sql = build_query(team)
    rows = list(User.objects.raw(sql))  # SINK
    return HttpResponse(str(len(rows)))
