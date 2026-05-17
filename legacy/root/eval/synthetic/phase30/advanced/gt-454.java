import java.util.ArrayList;
import java.util.List;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestTemplate;

@RestController
class Gt454Controller {
    private final List<String> webhooks = new ArrayList<>();
    private final RestTemplate restTemplate = new RestTemplate();

    @PostMapping("/webhooks")
    void add(@RequestParam String url) {
        webhooks.add(url);
    }

    void triggerAll() {
        for (String url : webhooks) {
            restTemplate.getForObject(url, String.class); // sink
        }
    }
}
