import org.springframework.expression.spel.standard.SpelExpressionParser;
import org.springframework.expression.spel.support.SimpleEvaluationContext;

class DemoController {
    Object handler(javax.servlet.http.HttpServletRequest request) {
        SpelExpressionParser parser = new SpelExpressionParser();
        SimpleEvaluationContext context = SimpleEvaluationContext.forReadOnlyDataBinding().build();
        return parser.parseExpression(request.getParameter("expr")).getValue(context);
    }
}
