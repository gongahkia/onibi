def admin_delete_user(request, user_id):
    User.objects.get(id=user_id).delete()
