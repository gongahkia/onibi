import javax.el.ExpressionFactory;

class DemoController {
    Object handler(javax.servlet.http.HttpServletRequest request, Object context) {
        ExpressionFactory factory = ExpressionFactory.newInstance();
        return factory.createValueExpression(context, "${" + request.getParameter("expr") + "}", Object.class);
    }
}
