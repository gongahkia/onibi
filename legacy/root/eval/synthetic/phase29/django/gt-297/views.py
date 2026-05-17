from django.http import HttpResponse
from services import execute_search

def search(request):
    incoming = request.GET["name"]
    rows = execute_search(incoming)
    return HttpResponse(str(rows))
