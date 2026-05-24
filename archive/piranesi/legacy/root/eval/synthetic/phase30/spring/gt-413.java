import java.io.ByteArrayInputStream;
import java.io.ObjectInputStream;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt413Controller {
    @PostMapping("/import")
    Object load(@RequestBody byte[] body) throws Exception {
        ObjectInputStream input = new ObjectInputStream(new ByteArrayInputStream(body));
        return input.readObject(); // sink
    }
}
