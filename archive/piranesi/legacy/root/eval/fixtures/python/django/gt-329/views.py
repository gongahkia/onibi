from django import forms
from django.http import HttpResponse
from app.models import Profile

class ProfileForm(forms.ModelForm):
    class Meta:
        model = Profile

def persist_profile(data, instance):
    form = ProfileForm(data, instance=instance)
    if form.is_valid():
        return form.save()  # SINK
    return None

def profile_view(request):
    persist_profile(request.POST, request.user.profile)
    return HttpResponse("ok")
