package com.example.demo;

import java.util.List;
import java.util.Map;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.security.access.annotation.Secured;
import org.springframework.security.access.prepost.PreAuthorize;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class UserController {
    private static final RowMapper<String> USER_ROW_MAPPER = (rs, rowNum) -> rs.getString("name");

    private final JdbcTemplate jdbcTemplate;

    UserController(JdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    @PreAuthorize("hasRole('ADMIN')")
    @GetMapping("/users")
    List<String> lookup(@RequestParam String name) {
        return jdbcTemplate.query(
            "SELECT name FROM users WHERE name = '" + name + "'",
            USER_ROW_MAPPER
        );
    }

    @Secured("ROLE_WRITER")
    @PostMapping("/users")
    List<Map<String, Object>> create(@RequestBody String email) {
        return jdbcTemplate.queryForList(
            "SELECT * FROM users WHERE email = '" + email + "'"
        );
    }
}
