from django.http import HttpResponse
from presenter import render_notice

def banner(request):
    return HttpResponse(render_notice(request.GET["notice"]))
