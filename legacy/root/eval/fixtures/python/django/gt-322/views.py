from django.http import JsonResponse
from app.models import User

def user_detail(request):
    user_id = request.GET["id"]
    rows = list(User.objects.raw(f"SELECT * FROM app_user WHERE id = {user_id}"))  # SINK
    return JsonResponse({"count": len(rows)})
