import com.fasterxml.jackson.databind.ObjectMapper;
import java.util.Map;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt407Controller {
    private final ObjectMapper mapper = new ObjectMapper();

    @PostMapping("/members")
    Gt407User create(@RequestBody Map<String, Object> payload) {
        return mapper.convertValue(payload, Gt407User.class); // sink
    }
}

class Gt407User {
    public String username;
    public boolean locked;
    public boolean staff;
}
