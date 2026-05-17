import java.io.ObjectInputStream;

class Gt435 {
    static void main(String[] args) throws Exception {
        ObjectInputStream input = new ObjectInputStream(System.in);
        input.readObject(); // sink
    }
}
