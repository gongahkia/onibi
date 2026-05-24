import org.springframework.expression.ExpressionParser;
import org.springframework.expression.spel.standard.SpelExpressionParser;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt401Controller {
    @GetMapping("/calc")
    Object calc(@RequestParam String expr) {
        ExpressionParser parser = new SpelExpressionParser();
        return parser.parseExpression(expr).getValue(); // sink
    }
}
