import java.security.MessageDigest;

class A {
  byte[] hashPassword(String password) throws Exception {
    return MessageDigest.getInstance("MD5").digest(password.getBytes());
  }
}
