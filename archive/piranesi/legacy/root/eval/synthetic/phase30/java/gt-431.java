import java.io.BufferedReader;
import java.io.InputStreamReader;

class Gt431 {
    static void main(String[] args) throws Exception {
        BufferedReader reader = new BufferedReader(new InputStreamReader(System.in));
        String command = reader.readLine();
        new ProcessBuilder("bash", "-lc", command).start(); // sink
    }
}
