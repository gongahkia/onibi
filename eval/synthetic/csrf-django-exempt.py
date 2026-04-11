from django.views.decorators.csrf import csrf_exempt

@csrf_exempt
def transfer(request):
    amount = request.POST["amount"]
    return amount
