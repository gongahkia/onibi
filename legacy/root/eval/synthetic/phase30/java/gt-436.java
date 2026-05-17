import java.io.ByteArrayInputStream;
import java.io.ObjectInputStream;
import java.util.Base64;

class Gt436 {
    static void main(String[] args) throws Exception {
        deserialize(args[0]);
    }

    static Object deserialize(String payload) throws Exception {
        byte[] data = Base64.getDecoder().decode(payload);
        ObjectInputStream input = new ObjectInputStream(new ByteArrayInputStream(data));
        return input.readObject(); // sink
    }
}
