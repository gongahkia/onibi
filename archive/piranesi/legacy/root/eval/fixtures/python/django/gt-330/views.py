from django import forms
from django.http import HttpResponse
from app.models import User

class AdminForm(forms.ModelForm):
    class Meta:
        model = User
        exclude = []

def admin_update(request):
    form = AdminForm(request.POST, instance=request.user)
    if form.is_valid():
        form.save()  # SINK
    return HttpResponse("ok")
