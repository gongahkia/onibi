import java.beans.XMLDecoder;
import java.io.BufferedInputStream;

class Gt433 {
    static void main(String[] args) {
        XMLDecoder decoder = new XMLDecoder(new BufferedInputStream(System.in));
        decoder.readObject(); // sink
    }
}
