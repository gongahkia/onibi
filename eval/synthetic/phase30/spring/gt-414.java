import java.io.ByteArrayInputStream;
import java.io.ObjectInputStream;
import java.util.Base64;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt414Controller {
    @GetMapping("/restore")
    Object restore(@RequestParam String payload) throws Exception {
        byte[] data = Base64.getDecoder().decode(payload);
        ObjectInputStream input = new ObjectInputStream(new ByteArrayInputStream(data));
        return input.readObject(); // sink
    }
}
