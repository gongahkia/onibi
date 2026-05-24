public class SecurityConfig {
    void configure(HttpSecurity http) throws Exception {
        http.sessionManagement().sessionFixation().none();
    }
}
