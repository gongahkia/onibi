def login_view(request):
    user = User.objects.get(username=request.POST["username"])
    if user.password == request.POST["password"]:
        login(request, user)
