import java.io.ByteArrayInputStream;
import java.io.ObjectInputStream;
import org.springframework.stereotype.Service;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt416Controller {
    private final Gt416ImportService service = new Gt416ImportService();

    @PostMapping("/jobs")
    Object importJob(@RequestBody byte[] payload) throws Exception {
        return service.read(payload);
    }
}

@Service
class Gt416ImportService {
    Object read(byte[] payload) throws Exception {
        ObjectInputStream input = new ObjectInputStream(new ByteArrayInputStream(payload));
        return input.readObject(); // sink
    }
}
