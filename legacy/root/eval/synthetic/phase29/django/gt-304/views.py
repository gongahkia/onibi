from django.http import HttpResponse
from services import load_saved_doc, remember_doc

def save_document(request):
    remember_doc(request.POST["user_id"], request.POST["doc"])
    return HttpResponse("saved")

def show_document(request):
    return HttpResponse(load_saved_doc(request.GET["user_id"]))
