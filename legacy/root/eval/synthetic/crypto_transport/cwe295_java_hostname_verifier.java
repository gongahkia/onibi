import javax.net.ssl.HostnameVerifier;

class A {
  HostnameVerifier insecure = (host, session) -> { return true; };
}
