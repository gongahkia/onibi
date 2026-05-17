import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt400Controller {
    @Autowired JdbcTemplate jdbc;

    @GetMapping("/audit")
    Object audit(@RequestParam String sort) {
        String sql = "SELECT id, actor, created_at FROM audit_log ORDER BY " + sort;
        return jdbc.queryForList(sql); // sink
    }
}
