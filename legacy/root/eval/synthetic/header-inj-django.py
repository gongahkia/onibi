from django.http import HttpResponse

def handler(request):
    response = HttpResponse("ok")
    response["X-Custom"] = request.GET["val"]
    return response
