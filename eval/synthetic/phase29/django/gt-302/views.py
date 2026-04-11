from django.http import HttpResponse
from services import save_clause, show_clause

def save_report(request):
    save_clause(request.POST["report_id"], request.POST["sort"])
    return HttpResponse("saved")

def show_report(request):
    rows = show_clause(request.GET["report_id"])
    return HttpResponse(str(rows))
