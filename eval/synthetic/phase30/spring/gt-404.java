import org.springframework.expression.ExpressionParser;
import org.springframework.expression.spel.standard.SpelExpressionParser;
import org.springframework.stereotype.Service;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt404Controller {
    private final Gt404ExpressionService service = new Gt404ExpressionService();

    @GetMapping("/selector")
    Object selector(@RequestParam String expr) {
        return service.evaluate(expr);
    }
}

@Service
class Gt404ExpressionService {
    Object evaluate(String expr) {
        ExpressionParser parser = new SpelExpressionParser();
        return parser.parseExpression(expr).getValue(); // sink
    }
}
