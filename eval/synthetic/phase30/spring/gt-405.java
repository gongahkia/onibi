import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt405Controller {
    private final Gt405Repository repo = new Gt405Repository();

    @PutMapping("/profile")
    Gt405User update(@RequestBody Gt405User user) {
        return repo.save(user); // sink
    }
}

class Gt405User {
    public Long id;
    public String name;
    public boolean admin;
}

class Gt405Repository {
    Gt405User save(Gt405User user) {
        return user;
    }
}
