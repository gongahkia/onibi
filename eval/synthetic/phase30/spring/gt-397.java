import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt397Controller {
    @Autowired JdbcTemplate jdbc;

    @GetMapping("/users")
    Object list(@RequestParam String id) {
        String sql = "SELECT * FROM users WHERE id = '" + id + "'";
        return jdbc.queryForList(sql); // sink
    }
}
