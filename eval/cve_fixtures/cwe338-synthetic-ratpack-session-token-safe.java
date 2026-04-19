import java.security.SecureRandom;

final class SessionTokenFactory {
    private final SecureRandom secureRandom = new SecureRandom();

    String nextToken() {
        byte[] bytes = new byte[16];
        secureRandom.nextBytes(bytes);
        return java.util.Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
    }
}
