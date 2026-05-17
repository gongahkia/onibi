import java.nio.file.Files;
import java.nio.file.Path;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;

@RestController
class Gt449Controller {
    @PostMapping("/themes")
    String upload(@RequestParam MultipartFile file, @RequestParam String name) throws Exception {
        Path destination = Path.of("/app/templates/" + name);
        Files.copy(file.getInputStream(), destination); // sink
        return "ok";
    }
}
