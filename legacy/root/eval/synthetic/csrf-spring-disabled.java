import org.springframework.context.annotation.Configuration;

@Configuration
public class SecurityConfig {
    void configure(HttpSecurity http) throws Exception {
        http.csrf().disable();
    }
}
