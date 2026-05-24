import java.security.KeyPairGenerator;

class A {
  void weak() throws Exception {
    KeyPairGenerator kpg = KeyPairGenerator.getInstance("RSA");
    kpg.initialize(1024);
  }
}
