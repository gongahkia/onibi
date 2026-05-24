import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt398Controller {
    @Autowired JdbcTemplate jdbc;

    @GetMapping("/lookup")
    Object lookup(@RequestParam String email) {
        String sql = String.format("SELECT * FROM users WHERE email = '%s'", email);
        return jdbc.queryForMap(sql); // sink
    }
}
