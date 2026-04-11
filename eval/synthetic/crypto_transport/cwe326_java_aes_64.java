import javax.crypto.KeyGenerator;

class A {
  void weak() throws Exception {
    KeyGenerator keyGen = KeyGenerator.getInstance("AES");
    keyGen.init(64);
  }
}
