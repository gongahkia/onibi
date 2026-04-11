import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestTemplate;

@RestController
class Gt409Controller {
    @Autowired RestTemplate restTemplate;

    @GetMapping("/fetch")
    String fetch(@RequestParam String url) {
        return restTemplate.getForObject(url, String.class); // sink
    }
}
