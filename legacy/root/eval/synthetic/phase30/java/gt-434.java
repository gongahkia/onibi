import java.beans.XMLDecoder;
import java.io.ByteArrayInputStream;
import java.util.Base64;

class Gt434 {
    static void main(String[] args) {
        byte[] xml = Base64.getDecoder().decode(args[0]);
        XMLDecoder decoder = new XMLDecoder(new ByteArrayInputStream(xml));
        decoder.readObject(); // sink
    }
}
