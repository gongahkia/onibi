from django.http import HttpResponse
from services import execute_search

def search(request):
    rows = execute_search(request.GET["name"])
    return HttpResponse(str(rows))
