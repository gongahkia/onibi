import org.springframework.web.bind.annotation.*;

@RestController
class DemoController {
    String handler(javax.servlet.http.HttpServletRequest request, Object ldapTemplate) {
        String filter = "(&(uid=" + request.getParameter("username") + ")(objectClass=person))";
        return String.valueOf(filter);
    }
}
