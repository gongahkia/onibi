from django.http import HttpResponse
from django.views.decorators.csrf import csrf_exempt

def close_order(order_id: str) -> str:
    return order_id

@csrf_exempt  # SINK
def close(request):
    close_order(request.POST["order_id"])
    return HttpResponse("closed")
