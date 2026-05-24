import java.io.BufferedReader;
import java.io.InputStreamReader;

class Gt428 {
    static void main(String[] args) throws Exception {
        BufferedReader reader = new BufferedReader(new InputStreamReader(System.in));
        String host = reader.readLine();
        Runtime.getRuntime().exec("ping -c 1 " + host); // sink
    }
}
