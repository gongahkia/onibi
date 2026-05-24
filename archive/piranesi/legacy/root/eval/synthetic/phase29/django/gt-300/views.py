from django.http import HttpResponse
from services import open_document

def document(request):
    body = open_document(request.GET["doc"])
    return HttpResponse(body)
