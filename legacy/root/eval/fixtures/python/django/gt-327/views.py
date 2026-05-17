from django.http import HttpResponse
from app.models import User

def ordered(request):
    order_key = request.GET["sort"]
    rows = User.objects.extra(order_by=[order_key])  # SINK
    return HttpResponse(str(rows.count()))
