from django.http import JsonResponse
from app.models import User

def search(request):
    term = request.GET["q"]
    rows = User.objects.extra(where=[f"name LIKE '%%{term}%%'"])  # SINK
    return JsonResponse({"count": rows.count()})
