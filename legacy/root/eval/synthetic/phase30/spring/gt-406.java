import org.springframework.beans.BeanUtils;
import org.springframework.web.bind.annotation.PatchMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt406Controller {
    @PatchMapping("/users")
    Gt406User patch(@RequestBody Gt406Patch payload) {
        Gt406User user = new Gt406User();
        BeanUtils.copyProperties(payload, user); // sink
        return user;
    }
}

class Gt406Patch {
    public String email;
    public String role;
}

class Gt406User {
    public String email;
    public String role;
}
