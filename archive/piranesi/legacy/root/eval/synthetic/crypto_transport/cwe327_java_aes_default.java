import javax.crypto.Cipher;

class A {
  Cipher insecure() throws Exception {
    return Cipher.getInstance("AES");
  }
}
