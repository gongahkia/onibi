package com.example.demo;

import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;

class UserControllerTest {
    private static final RowMapper<String> USER_ROW_MAPPER = (rs, rowNum) -> rs.getString("name");

    void ignored(JdbcTemplate jdbcTemplate, String injected) {
        jdbcTemplate.query(
            "SELECT name FROM users WHERE name = '" + injected + "'",
            USER_ROW_MAPPER
        );
    }
}
