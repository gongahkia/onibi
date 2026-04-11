from django.http import JsonResponse

def order_detail(request, pk):
    order = Order.objects.get(pk=pk)
    return JsonResponse({"id": order.id})
