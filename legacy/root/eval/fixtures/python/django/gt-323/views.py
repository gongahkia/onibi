from django.http import JsonResponse
from app.models import User

def run_status_lookup(status: str):
    return list(User.objects.raw(f"SELECT * FROM app_user WHERE status = '{status}'"))  # SINK

def status_view(request):
    status = request.POST["status"]
    rows = run_status_lookup(status.strip())
    return JsonResponse({"count": len(rows)})
