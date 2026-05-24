import org.springframework.expression.ExpressionParser;
import org.springframework.expression.spel.standard.SpelExpressionParser;
import org.springframework.expression.spel.support.StandardEvaluationContext;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt403Controller {
    @GetMapping("/policy")
    Boolean decide(@RequestParam String rule) {
        ExpressionParser parser = new SpelExpressionParser();
        StandardEvaluationContext ctx = new StandardEvaluationContext(new Gt403Policy());
        return parser.parseExpression(rule).getValue(ctx, Boolean.class); // sink
    }
}

class Gt403Policy {
    public boolean isAdmin() {
        return false;
    }
}
