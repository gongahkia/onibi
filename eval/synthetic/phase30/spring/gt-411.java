import java.util.Map;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestTemplate;

@RestController
class Gt411Controller {
    @Autowired RestTemplate restTemplate;

    @PostMapping("/callback")
    String callback(@RequestParam String callbackUrl) {
        return restTemplate.postForEntity(callbackUrl, Map.of("status", "ready"), String.class).getBody(); // sink
    }
}
