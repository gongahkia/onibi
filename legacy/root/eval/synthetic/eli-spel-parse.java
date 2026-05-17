import org.springframework.web.bind.annotation.*;
import org.springframework.expression.spel.standard.SpelExpressionParser;

@RestController
class DemoController {
    Object handler(javax.servlet.http.HttpServletRequest request) {
        SpelExpressionParser parser = new SpelExpressionParser();
        return parser.parseExpression(request.getParameter("expr"));
    }
}
