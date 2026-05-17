import java.io.ObjectInputStream;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;

@RestController
class Gt415Controller {
    @PostMapping("/upload")
    Object upload(@RequestParam MultipartFile file) throws Exception {
        ObjectInputStream input = new ObjectInputStream(file.getInputStream());
        return input.readObject(); // sink
    }
}
