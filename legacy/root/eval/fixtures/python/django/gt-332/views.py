from django.http import JsonResponse
from django.utils.decorators import method_decorator
from django.views import View
from django.views.decorators.csrf import csrf_exempt

@method_decorator(csrf_exempt, name="dispatch")  # SINK
class BillingWebhook(View):
    def post(self, request):
        return JsonResponse({"charged": request.POST["invoice"]})
