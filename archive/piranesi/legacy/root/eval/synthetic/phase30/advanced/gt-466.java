class Gt466Auth {
    boolean authenticate(String user, String password) {
        String expectedUser = loadUser();
        if (!expectedUser.equals(user)) { // sink
            return false;
        }
        return loadPassword().equals(password);
    }

    String loadUser() {
        return "admin";
    }

    String loadPassword() {
        return "secret";
    }
}
