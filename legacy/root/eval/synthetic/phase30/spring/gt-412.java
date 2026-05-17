import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestTemplate;

@RestController
class Gt412Controller {
    @Autowired Gt412ProxyService proxy;

    @GetMapping("/preview")
    String preview(@RequestParam String feed) {
        return proxy.read(feed);
    }
}

@Service
class Gt412ProxyService {
    private final RestTemplate restTemplate = new RestTemplate();

    String read(String feed) {
        String target = feed.trim();
        return restTemplate.getForEntity(target, String.class).getBody(); // sink
    }
}
