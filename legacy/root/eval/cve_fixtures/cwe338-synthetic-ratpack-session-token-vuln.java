import java.util.Random;

final class SessionTokenFactory {
    String nextToken(String userId) {
        long predictableSeed = userId.hashCode();
        Random random = new Random(predictableSeed); // vulnerable: deterministic token stream
        return Long.toHexString(random.nextLong());
    }
}
