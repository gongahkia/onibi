import org.springframework.expression.ExpressionParser;
import org.springframework.expression.ParserContext;
import org.springframework.expression.spel.standard.SpelExpressionParser;
import org.springframework.expression.spel.support.StandardEvaluationContext;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt402Controller {
    @GetMapping("/render")
    Object render(@RequestParam String fragment) {
        ExpressionParser parser = new SpelExpressionParser();
        ParserContext template = new org.springframework.expression.common.TemplateParserContext();
        StandardEvaluationContext ctx = new StandardEvaluationContext();
        return parser.parseExpression(fragment, template).getValue(ctx); // sink
    }
}
