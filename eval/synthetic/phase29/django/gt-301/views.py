from django.http import HttpResponse
from services import proxy_url

def proxy(request):
    response = proxy_url(request.GET["url"])
    return HttpResponse(str(response))
