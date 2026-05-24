from django import forms
from django.http import HttpResponse
from app.models import Account

class AccountForm(forms.ModelForm):
    class Meta:
        model = Account

def update_account(request):
    form = AccountForm(request.POST, instance=request.user.account)
    if form.is_valid():
        form.save()  # SINK
    return HttpResponse("ok")
