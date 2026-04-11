function authenticate(user, providedPassword) {
  if (user.password === providedPassword) {
    return true;
  }
  return false;
}
