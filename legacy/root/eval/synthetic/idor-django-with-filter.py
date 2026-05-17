from django.http import JsonResponse

def order_detail(request, pk):
    order = Order.objects.get(pk=pk, user=request.user)
    return JsonResponse({"id": order.id})
