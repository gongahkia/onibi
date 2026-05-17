import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Service;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt399Controller {
    @Autowired Gt399QueryBuilder builder;
    @Autowired JdbcTemplate jdbc;

    @GetMapping("/accounts")
    Object account(@RequestParam String email) {
        String sql = builder.byEmail(email);
        return jdbc.queryForList(sql); // sink
    }
}

@Service
class Gt399QueryBuilder {
    String byEmail(String email) {
        return "SELECT * FROM accounts WHERE email = '" + email.trim() + "'";
    }
}
