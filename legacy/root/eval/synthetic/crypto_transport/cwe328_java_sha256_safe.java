import java.security.MessageDigest;

class A {
  byte[] hashPassword(String password) throws Exception {
    return MessageDigest.getInstance("SHA-256").digest(password.getBytes());
  }
}
