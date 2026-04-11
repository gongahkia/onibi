import java.net.URI;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpMethod;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestTemplate;

@RestController
class Gt410Controller {
    @Autowired RestTemplate restTemplate;

    @GetMapping("/probe")
    String probe(@RequestParam String target) {
        return restTemplate.exchange(URI.create(target), HttpMethod.GET, HttpEntity.EMPTY, String.class).getBody(); // sink
    }
}
