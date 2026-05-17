from django.http import HttpResponse
from services import build_probe

def probe(request):
    header = request.headers["X-Command"]
    build_probe(header)
    return HttpResponse("ok")
