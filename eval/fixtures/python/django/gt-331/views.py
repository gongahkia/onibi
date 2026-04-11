from django.http import JsonResponse
from django.views.decorators.csrf import csrf_exempt

@csrf_exempt  # SINK
def transfer(request):
    amount = request.POST["amount"]
    recipient = request.POST["to"]
    return JsonResponse({"amount": amount, "recipient": recipient})
